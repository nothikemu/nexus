// Package dashboard is the `nexus` live dashboard: database health,
// throughput, migrations and activity, refreshed every two seconds, with the
// table explorer one keypress away.
package dashboard

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
	"github.com/nothikemu/nexus/internal/tui/browser"
	"github.com/nothikemu/nexus/internal/tui/tuikit"
	"github.com/nothikemu/nexus/internal/ui"
)

const (
	refreshEvery = 2 * time.Second
	schemaEvery  = 15 * time.Second
	seriesLen    = 60
)

type tab int

const (
	tabOverview tab = iota
	tabTables
	tabMigrations
	tabActivity
)

var tabNames = []string{"overview", "tables", "migrations", "activity"}

// Run opens the dashboard full-screen.
func Run(ctx context.Context, t *ui.Theme, src Source) error {
	m := newModel(ctx, t, src)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if m.pool != nil {
		m.pool.Close()
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

type refreshMsg struct{}

type model struct {
	ctx context.Context
	t   *ui.Theme
	src Source

	pool       *pgxpool.Pool
	cur        *sample
	prev       *stats.Overview
	lastSchema time.Time
	fetching   bool
	waking     bool
	wakeErr    error

	commits, reads, writes, conns series

	tab      tab
	selected [4]int
	browser  *browser.Model

	frame  int
	width  int
	height int
}

func newModel(ctx context.Context, t *ui.Theme, src Source) *model {
	m := &model{ctx: ctx, t: t, src: src, width: 100, height: 30}
	for _, s := range []*series{&m.commits, &m.reads, &m.writes, &m.conns} {
		s.size = seriesLen
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.refresh(true), tuikit.Tick(150*time.Millisecond))
}

func (m *model) refresh(schema bool) tea.Cmd {
	if m.fetching {
		return nil
	}
	m.fetching = true
	var snap *introspect.Snapshot
	if m.cur != nil {
		snap = m.cur.snap
	}
	return fetch(m.ctx, m.src, m.pool, snap, schema)
}

func (m *model) online() bool { return m.cur != nil && m.cur.err == nil && m.cur.overview != nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// An open browser owns the screen.
	if m.browser != nil {
		switch msg := msg.(type) {
		case browser.BackMsg:
			m.browser = nil
			return m, m.refresh(false)
		case tea.WindowSizeMsg:
			m.width, m.height = msg.Width, msg.Height
		case sampleMsg:
			m.absorb(msg.s)
			return m, tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshMsg{} })
		case refreshMsg:
			return m, tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshMsg{} })
		}
		_, cmd := m.browser.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tuikit.TickMsg:
		m.frame++
		return m, tuikit.Tick(150 * time.Millisecond)
	case refreshMsg:
		return m, m.refresh(time.Since(m.lastSchema) > schemaEvery)
	case sampleMsg:
		m.absorb(msg.s)
		return m, tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshMsg{} })
	case wokeMsg:
		m.waking = false
		m.wakeErr = msg.err
		return m, m.refresh(true)
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) absorb(s *sample) {
	m.fetching = false
	if s.err != nil {
		m.cur = s
		if m.pool != nil {
			m.pool.Close()
			m.pool = nil
		}
		m.prev = nil
		return
	}
	m.pool = s.pool
	if s.snap != nil && (m.cur == nil || s.snap != m.cur.snap) {
		m.lastSchema = s.at
	}
	if m.prev != nil {
		r := stats.RatesBetween(m.prev, s.overview)
		m.commits.push(r.Commits)
		m.reads.push(r.RowsRead)
		m.writes.push(r.RowsWritten)
	}
	m.conns.push(float64(s.overview.Connections))
	m.prev = s.overview
	m.cur = s
}

func (m *model) listLen() int {
	if !m.online() {
		return 0
	}
	switch m.tab {
	case tabTables:
		if m.cur.snap != nil {
			return len(m.cur.snap.Tables)
		}
	case tabMigrations:
		if m.cur.migration != nil {
			return len(m.cur.migration.Entries)
		}
	case tabActivity:
		return len(m.cur.sessions)
	}
	return 0
}

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "1", "2", "3", "4":
		m.tab = tab(k.String()[0] - '1')
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % 4
	case "shift+tab", "left", "h":
		m.tab = (m.tab + 3) % 4
	case "up", "k":
		if m.selected[m.tab] > 0 {
			m.selected[m.tab]--
		}
	case "down", "j":
		if m.selected[m.tab] < m.listLen()-1 {
			m.selected[m.tab]++
		}
	case "r":
		return m, m.refresh(true)
	case "w":
		if !m.online() && m.src.Wake != nil && !m.waking {
			m.waking = true
			m.wakeErr = nil
			return m, wake(m.ctx, m.src)
		}
	case "enter":
		if m.tab == tabTables && m.online() && m.cur.snap != nil && len(m.cur.snap.Tables) > 0 {
			tb := m.cur.snap.Tables[min(m.selected[tabTables], len(m.cur.snap.Tables)-1)]
			o := browser.Options{Theme: m.t, Pool: m.pool, Snapshot: m.cur.snap, Table: tb, Embedded: true}
			if m.src.Protected {
				o.ReadOnly, o.ReadOnlyReason = true, "editing is off on "+m.src.Env+" — it's a protected environment."
			}
			m.browser = browser.New(m.ctx, o)
			init := m.browser.Init()
			_, cmd := m.browser.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, tea.Batch(init, cmd)
		}
	}
	return m, nil
}

// Package browser is the interactive table explorer: page through rows,
// search, filter with SQL, sort, inspect records and JSON, follow foreign
// keys, and edit cells by primary key.
package browser

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/rows"
	"github.com/nothikemu/nexus/internal/tui/tuikit"
	"github.com/nothikemu/nexus/internal/ui"
)

// Options configures the browser.
type Options struct {
	Theme    *ui.Theme
	Pool     *pgxpool.Pool
	Snapshot *introspect.Snapshot
	Table    *introspect.Table
	// ReadOnly disables editing (protected environments).
	ReadOnly       bool
	ReadOnlyReason string
	// Embedded makes quitting at the root emit BackMsg instead of tea.Quit,
	// so the dashboard can host the browser.
	Embedded bool
}

// BackMsg asks the host to close an embedded browser.
type BackMsg struct{}

// Run opens the browser full-screen and blocks until it closes.
func Run(ctx context.Context, o Options) error {
	m := New(ctx, o)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

type mode int

const (
	modeGrid mode = iota
	modeSearch
	modeFilter
	modeEdit
	modeConfirm
	modeDetail
)

// view is one entry in the navigation stack (following a foreign key pushes one).
type view struct {
	table     *introspect.Table
	where     string
	search    string
	order     []rows.OrderKey
	offset    int
	row, col  int
	colOffset int
	via       string // breadcrumb: how we got here
}

// Model is the browser's Bubble Tea model.
type Model struct {
	ctx context.Context
	o   Options
	t   *ui.Theme

	stack []*view
	mode  mode

	res     *pg.Result
	total   int64
	elapsed time.Duration
	loading bool
	seq     int
	loadErr error

	input   textinput.Model
	detail  viewport.Model
	pending *edit

	flash     string
	flashTone ui.Tone
	frame     int
	width     int
	height    int
}

// New creates a browser model.
func New(ctx context.Context, o Options) *Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.TextStyle = o.Theme.Text
	ti.PlaceholderStyle = o.Theme.Faint
	ti.Cursor.Style = o.Theme.Primary
	m := &Model{ctx: ctx, o: o, t: o.Theme, input: ti, width: 100, height: 30}
	m.stack = []*view{{table: o.Table}}
	return m
}

func (m *Model) top() *view { return m.stack[len(m.stack)-1] }

// pageSize is the number of rows that fit on screen.
func (m *Model) pageSize() int {
	n := m.height - 7
	if n < 3 {
		n = 3
	}
	return n
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return m.reload() }

func (m *Model) reload() tea.Cmd {
	m.seq++
	m.loading = true
	m.frame = 0
	return tea.Batch(load(m.ctx, m.o.Pool, m.top(), m.pageSize(), m.seq), tuikit.Tick(90*time.Millisecond))
}

func (m *Model) say(tone ui.Tone, s string) {
	m.flash, m.flashTone = s, tone
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		old := m.pageSize()
		m.width, m.height = msg.Width, msg.Height
		m.detail.Width, m.detail.Height = msg.Width-4, msg.Height-5
		if m.pageSize() != old && m.res != nil {
			return m, m.reload()
		}
		return m, nil
	case tuikit.TickMsg:
		if m.loading {
			m.frame++
			return m, tuikit.Tick(90 * time.Millisecond)
		}
		return m, nil
	case loadedMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.loading = false
		m.loadErr = msg.err
		if msg.err == nil {
			m.res, m.total, m.elapsed = msg.res, msg.total, msg.elapsed
			v := m.top()
			if v.row >= len(m.res.Rows) {
				v.row = max(0, len(m.res.Rows)-1)
			}
			if v.col >= len(m.res.Columns) {
				v.col = max(0, len(m.res.Columns)-1)
			}
		}
		return m, nil
	case savedMsg:
		if msg.err != nil {
			m.say(ui.ToneError, errText(msg.err))
			return m, nil
		}
		m.say(ui.ToneSuccess, ui.SayFirst(ui.MomentNice)+" saved.")
		return m, m.reload()
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeSearch, modeFilter, modeEdit:
		return m.inputKey(k)
	case modeConfirm:
		return m.confirmKey(k)
	case modeDetail:
		switch k.String() {
		case "esc", "enter", "q", "backspace":
			m.mode = modeGrid
			return m, nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(k)
		return m, cmd
	}
	return m.gridKey(k)
}

func (m *Model) gridKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.top()
	rowsOnPage := 0
	if m.res != nil {
		rowsOnPage = len(m.res.Rows)
	}
	m.flash = ""
	switch k.String() {
	case "q":
		if m.o.Embedded && len(m.stack) == 1 {
			return m, func() tea.Msg { return BackMsg{} }
		}
		return m, tea.Quit
	case "esc", "backspace", "b":
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
			return m, m.reload()
		}
		if v.search != "" || v.where != "" {
			v.search, v.where, v.offset = "", "", 0
			return m, m.reload()
		}
		if k.String() == "esc" && m.o.Embedded {
			return m, func() tea.Msg { return BackMsg{} }
		}
	case "up", "k":
		if v.row > 0 {
			v.row--
		} else if v.offset > 0 {
			v.offset = max(0, v.offset-m.pageSize())
			v.row = m.pageSize() - 1
			return m, m.reload()
		}
	case "down", "j":
		if v.row < rowsOnPage-1 {
			v.row++
		} else if int64(v.offset+rowsOnPage) < m.total {
			v.offset += m.pageSize()
			v.row = 0
			return m, m.reload()
		}
	case "left", "h":
		if v.col > 0 {
			v.col--
		}
	case "right", "l":
		if m.res != nil && v.col < len(m.res.Columns)-1 {
			v.col++
		}
	case "home":
		v.col = 0
	case "end":
		if m.res != nil {
			v.col = len(m.res.Columns) - 1
		}
	case "pgdown", "n", "]":
		if int64(v.offset+m.pageSize()) < m.total {
			v.offset += m.pageSize()
			v.row = 0
			return m, m.reload()
		}
	case "pgup", "p", "[":
		if v.offset > 0 {
			v.offset = max(0, v.offset-m.pageSize())
			v.row = 0
			return m, m.reload()
		}
	case "r":
		return m, m.reload()
	case "/":
		return m, m.startInput(modeSearch, v.search, "search every column")
	case "f":
		return m, m.startInput(modeFilter, v.where, "SQL condition, e.g. created_at > now() - interval '1 day'")
	case "s":
		return m, m.cycleSort()
	case "enter", " ":
		if m.hasSelection() {
			m.openDetail()
		}
	case "g":
		return m, m.follow()
	case "e":
		return m, m.startEdit()
	}
	return m, nil
}

func (m *Model) hasSelection() bool {
	return m.res != nil && len(m.res.Rows) > 0
}

func (m *Model) startInput(md mode, value, placeholder string) tea.Cmd {
	m.mode = md
	m.input.SetValue(value)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) inputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.top()
	switch k.String() {
	case "esc":
		m.mode = modeGrid
		m.input.Blur()
		return m, nil
	case "ctrl+n":
		if m.mode == modeEdit {
			return m, m.confirmEdit(nil)
		}
	case "enter":
		val := m.input.Value()
		md := m.mode
		m.mode = modeGrid
		m.input.Blur()
		switch md {
		case modeSearch:
			v.search, v.offset, v.row = strings.TrimSpace(val), 0, 0
			return m, m.reload()
		case modeFilter:
			v.where, v.offset, v.row = strings.TrimSpace(val), 0, 0
			return m, m.reload()
		case modeEdit:
			return m, m.confirmEdit(&val)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m *Model) cycleSort() tea.Cmd {
	v := m.top()
	if m.res == nil || len(m.res.Columns) == 0 {
		return nil
	}
	col := m.res.Columns[v.col].Name
	switch {
	case len(v.order) == 1 && v.order[0].Column == col && !v.order[0].Desc:
		v.order = []rows.OrderKey{{Column: col, Desc: true}}
	case len(v.order) == 1 && v.order[0].Column == col:
		v.order = nil
	default:
		v.order = []rows.OrderKey{{Column: col}}
	}
	v.offset, v.row = 0, 0
	return m.reload()
}

// follow navigates along the foreign key on the selected column.
func (m *Model) follow() tea.Cmd {
	v := m.top()
	if !m.hasSelection() {
		return nil
	}
	col := m.res.Columns[v.col].Name
	fk := v.table.ForeignKeyFor(col)
	if fk == nil {
		m.say(ui.ToneInfo, col+" isn't a foreign key.")
		return nil
	}
	cell := m.res.Rows[v.row][v.col]
	if cell.Null {
		m.say(ui.ToneInfo, col+" is null — nothing to follow.")
		return nil
	}
	ref := m.o.Snapshot.Table(fk.RefQualified())
	if ref == nil {
		m.say(ui.ToneWarning, "can't see "+fk.RefQualified()+" (outside the configured schemas).")
		return nil
	}
	where := pg.QuoteIdent(fk.RefColumns[0]) + " = " + pg.QuoteLiteral(cell.Text)
	m.stack = append(m.stack, &view{table: ref, where: where, via: v.table.DisplayName() + "." + col})
	return m.reload()
}

func (m *Model) startEdit() tea.Cmd {
	v := m.top()
	if !m.hasSelection() {
		return nil
	}
	switch {
	case m.o.ReadOnly:
		m.say(ui.ToneWarning, m.o.ReadOnlyReason)
		return nil
	case !v.table.Writable():
		m.say(ui.ToneWarning, v.table.DisplayName()+" has no primary key, so rows can't be edited safely.")
		return nil
	}
	col := v.table.Column(m.res.Columns[v.col].Name)
	switch {
	case col == nil:
		return nil
	case col.Generated != "":
		m.say(ui.ToneInfo, col.Name+" is generated — PostgreSQL computes it.")
		return nil
	case col.Identity == "always":
		m.say(ui.ToneInfo, col.Name+" is an identity column (generated always).")
		return nil
	}
	cell := m.res.Rows[v.row][v.col]
	placeholder := "null"
	if !col.Nullable {
		placeholder = "required"
	}
	m.pending = &edit{column: col, pk: map[string]pg.Cell{}}
	for i, c := range m.res.Columns {
		if v.table.IsPrimaryKey(c.Name) {
			m.pending.pk[c.Name] = m.res.Rows[v.row][i]
		}
	}
	value := cell.Text
	if cell.Null {
		value = ""
	}
	return m.startInput(modeEdit, value, placeholder)
}

func (m *Model) confirmEdit(value *string) tea.Cmd {
	m.mode = modeConfirm
	m.input.Blur()
	if m.pending == nil {
		m.mode = modeGrid
		return nil
	}
	m.pending.value = value
	return nil
}

func (m *Model) confirmKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y", "enter":
		e := m.pending
		m.pending = nil
		m.mode = modeGrid
		m.say(ui.ToneNexus, "saving…")
		return m, save(m.ctx, m.o.Pool, m.top().table, e)
	default:
		m.pending = nil
		m.mode = modeGrid
		m.say(ui.ToneInfo, "cancelled — nothing changed.")
		return m, nil
	}
}

func errText(err error) string {
	if pe, ok := pg.AsPgError(err); ok {
		s := pe.Message
		if pe.Detail != "" {
			s += " — " + pe.Detail
		}
		return s
	}
	return err.Error()
}

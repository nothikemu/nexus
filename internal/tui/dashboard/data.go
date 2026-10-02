package dashboard

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
)

// Source is how the dashboard reaches the project. The CLI provides it.
type Source struct {
	Project   string
	Env       string
	Runtime   string // native, docker, external, or "" for remote targets
	Protected bool
	Schemas   []string
	// Connect returns a pool; it fails while the database is offline.
	Connect func(ctx context.Context) (*pgxpool.Pool, error)
	// Migrations returns migration status; nil when there's no project.
	Migrations func(ctx context.Context, pool *pgxpool.Pool) (*migrate.Status, error)
	// Wake starts the local database; nil when the target isn't local.
	Wake func(ctx context.Context) error
}

// sample is one refresh of everything the dashboard shows.
type sample struct {
	at        time.Time
	pool      *pgxpool.Pool
	err       error
	overview  *stats.Overview
	snap      *introspect.Snapshot
	findings  []stats.Finding
	sessions  []stats.Session
	migration *migrate.Status
	events    []migrate.Event
}

type sampleMsg struct{ s *sample }

type wokeMsg struct{ err error }

// fetch gathers a sample. The schema snapshot is reloaded only when asked,
// since it is the most expensive part.
func fetch(ctx context.Context, src Source, pool *pgxpool.Pool, prevSnap *introspect.Snapshot, withSchema bool) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		s := &sample{at: time.Now(), snap: prevSnap}
		if pool == nil {
			p, err := src.Connect(c)
			if err != nil {
				s.err = err
				return sampleMsg{s}
			}
			pool = p
			withSchema = true
		}
		s.pool = pool
		if s.overview, s.err = stats.LoadOverview(c, pool); s.err != nil {
			return sampleMsg{s}
		}
		if withSchema || s.snap == nil {
			if snap, err := introspect.Load(c, pool, src.Schemas); err == nil {
				s.snap = snap
			}
		}
		if s.snap != nil {
			s.findings = stats.Health(s.snap)
		}
		s.sessions, _ = stats.LoadActivity(c, pool, false)
		if src.Migrations != nil {
			s.migration, _ = src.Migrations(c, pool)
		}
		s.events, _ = migrate.RecentEvents(c, pool, 8)
		return sampleMsg{s}
	}
}

func wake(ctx context.Context, src Source) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return wokeMsg{err: src.Wake(c)}
	}
}

// series is a fixed-length ring of samples for sparklines.
type series struct {
	values []float64
	size   int
}

func (s *series) push(v float64) {
	s.values = append(s.values, v)
	if len(s.values) > s.size {
		s.values = s.values[len(s.values)-s.size:]
	}
}

func (s *series) last() float64 {
	if len(s.values) == 0 {
		return 0
	}
	return s.values[len(s.values)-1]
}

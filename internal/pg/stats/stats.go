// Package stats reads live database statistics and evaluates schema health.
package stats

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/pg"
)

// Overview is a point-in-time summary of a database.
type Overview struct {
	Version        string    `json:"version"`
	VersionNum     int       `json:"version_num"`
	Database       string    `json:"database"`
	User           string    `json:"user"`
	Size           int64     `json:"size_bytes"`
	Connections    int       `json:"connections"`
	Active         int       `json:"active_queries"`
	MaxConnections int       `json:"max_connections"`
	CacheHit       float64   `json:"cache_hit_ratio"` // -1 when there has been no I/O yet
	Commits        int64     `json:"commits"`
	Rollbacks      int64     `json:"rollbacks"`
	RowsRead       int64     `json:"rows_read"`
	RowsWritten    int64     `json:"rows_written"`
	Deadlocks      int64     `json:"deadlocks"`
	StartedAt      time.Time `json:"started_at"`
	InRecovery     bool      `json:"in_recovery"`
	SampledAt      time.Time `json:"sampled_at"`
}

const overviewSQL = `
select current_setting('server_version'),
       current_setting('server_version_num')::int,
       current_database(), current_user,
       pg_database_size(current_database()),
       (select count(*) from pg_stat_activity where datname = current_database())::int,
       (select count(*) from pg_stat_activity where datname = current_database()
          and state = 'active' and pid <> pg_backend_pid())::int,
       current_setting('max_connections')::int,
       case when d.blks_hit + d.blks_read = 0 then -1
            else d.blks_hit::float8 / (d.blks_hit + d.blks_read) end,
       d.xact_commit, d.xact_rollback,
       d.tup_returned + d.tup_fetched,
       d.tup_inserted + d.tup_updated + d.tup_deleted,
       d.deadlocks,
       pg_postmaster_start_time(),
       pg_is_in_recovery(),
       now()
from pg_stat_database d
where d.datname = current_database()`

// LoadOverview samples the current database.
func LoadOverview(ctx context.Context, q pg.Querier) (*Overview, error) {
	o := &Overview{}
	err := q.QueryRow(ctx, overviewSQL).Scan(&o.Version, &o.VersionNum, &o.Database, &o.User, &o.Size,
		&o.Connections, &o.Active, &o.MaxConnections, &o.CacheHit, &o.Commits, &o.Rollbacks,
		&o.RowsRead, &o.RowsWritten, &o.Deadlocks, &o.StartedAt, &o.InRecovery, &o.SampledAt)
	if err != nil {
		return nil, err
	}
	if i := strings.IndexByte(o.Version, ' '); i > 0 {
		o.Version = o.Version[:i]
	}
	return o, nil
}

// Rates are per-second deltas between two overview samples.
type Rates struct {
	Commits     float64 `json:"commits_per_sec"`
	RowsRead    float64 `json:"rows_read_per_sec"`
	RowsWritten float64 `json:"rows_written_per_sec"`
}

// RatesBetween computes per-second rates between two samples.
func RatesBetween(a, b *Overview) Rates {
	if a == nil || b == nil {
		return Rates{}
	}
	secs := b.SampledAt.Sub(a.SampledAt).Seconds()
	if secs <= 0 {
		return Rates{}
	}
	per := func(x, y int64) float64 {
		if y < x {
			return 0 // counters were reset
		}
		return float64(y-x) / secs
	}
	return Rates{
		Commits:     per(a.Commits+a.Rollbacks, b.Commits+b.Rollbacks),
		RowsRead:    per(a.RowsRead, b.RowsRead),
		RowsWritten: per(a.RowsWritten, b.RowsWritten),
	}
}

// Session is a backend from pg_stat_activity.
type Session struct {
	PID         int        `json:"pid"`
	User        string     `json:"user"`
	Application string     `json:"application"`
	Client      string     `json:"client"`
	State       string     `json:"state"`
	WaitEvent   string     `json:"wait_event,omitempty"`
	Query       string     `json:"query"`
	QueryStart  *time.Time `json:"query_start,omitempty"`
	Duration    float64    `json:"duration_seconds"`
}

// LoadActivity returns other sessions connected to the current database,
// busiest first. Idle sessions are included only when idle is true.
func LoadActivity(ctx context.Context, q pg.Querier, idle bool) ([]Session, error) {
	rows, err := q.Query(ctx, `
		select pid, coalesce(usename, ''), coalesce(application_name, ''),
		       coalesce(host(client_addr), 'local'), coalesce(state, ''),
		       coalesce(wait_event_type || ': ' || wait_event, ''),
		       coalesce(query, ''), query_start,
		       coalesce(extract(epoch from now() - query_start), 0)::float8
		from pg_stat_activity
		where datname = current_database()
		  and pid <> pg_backend_pid()
		  and backend_type = 'client backend'
		  and ($1 or state <> 'idle')
		order by state = 'active' desc, query_start nulls last
		limit 50`, idle)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var s Session
		err := r.Scan(&s.PID, &s.User, &s.Application, &s.Client, &s.State, &s.WaitEvent, &s.Query, &s.QueryStart, &s.Duration)
		return s, err
	})
}

package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/pg"
)

// Schema is where Nexus keeps its bookkeeping. It is deliberately not named
// "nexus": PostgreSQL's default search_path starts with "$user", so a schema
// named after a common role would silently capture unqualified objects.
const Schema = "nexus_meta"

// historyDDL creates Nexus's bookkeeping tables. It is idempotent.
const historyDDL = `
do $$
begin
  if not exists (select 1 from pg_namespace where nspname = 'nexus_meta') then
    create schema nexus_meta;
    comment on schema nexus_meta is 'Nexus internal state. Managed by the nexus CLI.';
  end if;
end
$$;

create table if not exists nexus_meta.migrations (
  version       text        primary key,
  name          text        not null,
  checksum      text        not null,
  applied_at    timestamptz not null default now(),
  duration_ms   integer     not null,
  applied_by    text        not null default current_user,
  nexus_version text        not null
);

create table if not exists nexus_meta.migration_events (
  id          bigint      generated always as identity primary key,
  version     text        not null,
  name        text        not null,
  action      text        not null check (action in ('apply', 'rollback', 'repair')),
  at          timestamptz not null default now(),
  duration_ms integer,
  actor       text        not null default current_user
);
`

// Record is a row of nexus_meta.migrations.
type Record struct {
	Version    string    `json:"version"`
	Name       string    `json:"name"`
	Checksum   string    `json:"checksum"`
	AppliedAt  time.Time `json:"applied_at"`
	DurationMs int       `json:"duration_ms"`
	AppliedBy  string    `json:"applied_by"`
}

// Event is a row of nexus_meta.migration_events.
type Event struct {
	Version    string    `json:"version"`
	Name       string    `json:"name"`
	Action     string    `json:"action"`
	At         time.Time `json:"at"`
	DurationMs *int      `json:"duration_ms,omitempty"`
	Actor      string    `json:"actor"`
}

func ensureHistory(ctx context.Context, q pg.Querier) error {
	_, err := q.Exec(ctx, historyDDL)
	return err
}

// historyExists reports whether nexus_meta.migrations exists, without creating it.
func historyExists(ctx context.Context, q pg.Querier) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select to_regclass('nexus_meta.migrations') is not null`).Scan(&ok)
	return ok, err
}

// Applied returns the applied migrations recorded in the database. A
// database Nexus has never migrated has no history (and gets no tables).
func Applied(ctx context.Context, q pg.Querier) ([]Record, error) {
	ok, err := historyExists(ctx, q)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		select version, name, checksum, applied_at, duration_ms, applied_by
		from nexus_meta.migrations order by applied_at, version`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Record, error) {
		var rec Record
		err := r.Scan(&rec.Version, &rec.Name, &rec.Checksum, &rec.AppliedAt, &rec.DurationMs, &rec.AppliedBy)
		return rec, err
	})
}

// RecentEvents returns the latest migration events, newest first.
func RecentEvents(ctx context.Context, q pg.Querier, limit int) ([]Event, error) {
	var ok bool
	if err := q.QueryRow(ctx, `select to_regclass('nexus_meta.migration_events') is not null`).Scan(&ok); err != nil || !ok {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		select version, name, action, at, duration_ms, actor
		from nexus_meta.migration_events order by at desc, id desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		err := r.Scan(&e.Version, &e.Name, &e.Action, &e.At, &e.DurationMs, &e.Actor)
		return e, err
	})
}

func recordApply(ctx context.Context, q pg.Querier, m *Migration, d time.Duration, nexusVersion string) error {
	ms := int(d.Milliseconds())
	if _, err := q.Exec(ctx, `
		insert into nexus_meta.migrations (version, name, checksum, duration_ms, nexus_version)
		values ($1, $2, $3, $4, $5)`, m.Version, m.Name, m.Checksum, ms, nexusVersion); err != nil {
		return err
	}
	return recordEvent(ctx, q, m.Version, m.Name, "apply", &ms)
}

func recordRollback(ctx context.Context, q pg.Querier, version, name string, d time.Duration) error {
	tag, err := q.Exec(ctx, `delete from nexus_meta.migrations where version = $1`, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("migration %s was not recorded as applied", version)
	}
	ms := int(d.Milliseconds())
	return recordEvent(ctx, q, version, name, "rollback", &ms)
}

func recordEvent(ctx context.Context, q pg.Querier, version, name, action string, ms *int) error {
	_, err := q.Exec(ctx, `
		insert into nexus_meta.migration_events (version, name, action, duration_ms)
		values ($1, $2, $3, $4)`, version, name, action, ms)
	return err
}

// lockKey is the advisory lock that serialises migrations across processes
// ("nexus" in ASCII).
const lockKey int64 = 0x6e65787573

// ErrLocked means another process holds the migration lock.
var ErrLocked = errors.New("another migration is in progress")

// lock takes the session-level migration lock, waiting up to timeout.
func lock(ctx context.Context, q pg.Querier, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		if err := q.QueryRow(ctx, `select pg_try_advisory_lock($1)`, lockKey).Scan(&ok); err != nil {
			return nil, err
		}
		if ok {
			return func() {
				// Use a fresh context: unlock must run even if ctx was cancelled.
				c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = q.Exec(c, `select pg_advisory_unlock($1)`, lockKey)
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrLocked
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

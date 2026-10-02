package migrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/schemadiff"
	"github.com/nothikemu/nexus/internal/sqltext"
)

// State of a migration relative to the database.
type State string

// Migration states.
const (
	StateApplied  State = "applied"
	StatePending  State = "pending"
	StateModified State = "modified" // applied, but the file changed since
	StateMissing  State = "missing"  // applied, but the file is gone
)

// Entry merges a migration file with its history record.
type Entry struct {
	Version    string     `json:"version"`
	Name       string     `json:"name"`
	State      State      `json:"state"`
	OutOfOrder bool       `json:"out_of_order,omitempty"`
	Empty      bool       `json:"empty,omitempty"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	DurationMs int        `json:"duration_ms,omitempty"`
	AppliedBy  string     `json:"applied_by,omitempty"`
	Reversible bool       `json:"reversible"`
	File       *Migration `json:"-"`
	Record     *Record    `json:"-"`
}

// Status summarises the migration state of a database.
type Status struct {
	Entries  []Entry `json:"migrations"`
	Applied  int     `json:"applied"`
	Pending  int     `json:"pending"`
	Modified int     `json:"modified"`
	Missing  int     `json:"missing"`
	Ordering int     `json:"out_of_order"`
}

// UpToDate reports whether there is nothing to apply and no drift.
func (s *Status) UpToDate() bool { return s.Pending == 0 && s.Modified == 0 && s.Missing == 0 }

// Latest returns the most recently applied entry, if any.
func (s *Status) Latest() *Entry {
	var latest *Entry
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.AppliedAt != nil && (latest == nil || e.AppliedAt.After(*latest.AppliedAt)) {
			latest = e
		}
	}
	return latest
}

// Engine applies migrations from a directory to a database.
type Engine struct {
	Pool         *pgxpool.Pool
	Dir          string
	Schemas      []string // schemas snapshotted for diffs
	NexusVersion string
	LockTimeout  time.Duration
}

// Observer receives progress events while migrations run.
type Observer interface {
	Started(m *Migration)
	Finished(m *Migration, d time.Duration, err error)
}

// nopObserver ignores events.
type nopObserver struct{}

func (nopObserver) Started(*Migration)                        {}
func (nopObserver) Finished(*Migration, time.Duration, error) {}

// Status compares migration files with the database history. It never
// writes to the database.
func (e *Engine) Status(ctx context.Context) (*Status, error) {
	files, err := LoadDir(e.Dir)
	if err != nil {
		return nil, err
	}
	records, err := Applied(ctx, e.Pool)
	if err != nil {
		return nil, err
	}
	return merge(files, records), nil
}

func merge(files []*Migration, records []Record) *Status {
	st := &Status{}
	recs := map[string]*Record{}
	maxApplied := ""
	for i := range records {
		r := &records[i]
		recs[r.Version] = r
		if maxApplied == "" || versionLess(maxApplied, r.Version) {
			maxApplied = r.Version
		}
	}
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Version] = true
		e := Entry{Version: f.Version, Name: f.Name, File: f, Reversible: f.HasDown, Empty: f.Empty}
		if r := recs[f.Version]; r != nil {
			at := r.AppliedAt
			e.AppliedAt, e.DurationMs, e.AppliedBy, e.Record = &at, r.DurationMs, r.AppliedBy, r
			if r.Checksum != f.Checksum {
				e.State = StateModified
				st.Modified++
			} else {
				e.State = StateApplied
			}
			st.Applied++
		} else {
			e.State = StatePending
			st.Pending++
			if maxApplied != "" && versionLess(f.Version, maxApplied) {
				e.OutOfOrder = true
				st.Ordering++
			}
		}
		st.Entries = append(st.Entries, e)
	}
	for i := range records {
		r := &records[i]
		if seen[r.Version] {
			continue
		}
		at := r.AppliedAt
		st.Entries = append(st.Entries, Entry{
			Version: r.Version, Name: r.Name, State: StateMissing,
			AppliedAt: &at, DurationMs: r.DurationMs, AppliedBy: r.AppliedBy, Record: r,
		})
		st.Missing++
		st.Applied++
	}
	sort.SliceStable(st.Entries, func(i, j int) bool { return versionLess(st.Entries[i].Version, st.Entries[j].Version) })
	return st
}

// ApplyOptions controls Apply.
type ApplyOptions struct {
	DryRun          bool   // run inside a transaction and roll back
	Target          string // stop after this version ("" = all)
	AllowOutOfOrder bool   // apply pending migrations older than the newest applied one
	SkipDiff        bool   // don't snapshot the schema before/after
}

// Applied migration with timing.
type AppliedMigration struct {
	Migration *Migration    `json:"migration"`
	Duration  time.Duration `json:"duration_ns"`
	Skipped   string        `json:"skipped,omitempty"` // reason, when not executed (dry-run of no-transaction)
}

// ApplyResult reports the outcome of Apply.
type ApplyResult struct {
	Applied []AppliedMigration `json:"applied"`
	Diff    *schemadiff.Diff   `json:"diff,omitempty"`
	DryRun  bool               `json:"dry_run"`
}

// DriftError means applied migrations were edited or deleted.
type DriftError struct {
	Modified []Entry
	Missing  []Entry
}

func (e *DriftError) Error() string {
	return fmt.Sprintf("%d applied migration(s) changed on disk and %d are missing", len(e.Modified), len(e.Missing))
}

// OrderError means pending migrations predate the newest applied one.
type OrderError struct {
	Entries []Entry
}

func (e *OrderError) Error() string {
	return fmt.Sprintf("%d pending migration(s) are older than the latest applied migration", len(e.Entries))
}

// EmptyError means a pending migration has no SQL yet.
type EmptyError struct {
	Migration *Migration
}

func (e *EmptyError) Error() string {
	return fmt.Sprintf("migration %s has an empty up section", e.Migration.ID())
}

// Error is a failure inside a migration's SQL.
type Error struct {
	Migration  *Migration
	Section    string // "up" or "down"
	SQL        string // the SQL that was sent
	LineOffset int    // file line of the first SQL line, minus one
	Err        error
}

func (e *Error) Error() string {
	return fmt.Sprintf("migration %s (%s) failed: %v", e.Migration.ID(), e.Section, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Plan returns the migrations Apply would run, after validating drift and ordering.
func (e *Engine) Plan(st *Status, opts ApplyOptions) ([]*Migration, error) {
	var drift DriftError
	var order OrderError
	var pending []*Migration
	for _, en := range st.Entries {
		switch en.State {
		case StateModified:
			drift.Modified = append(drift.Modified, en)
		case StateMissing:
			drift.Missing = append(drift.Missing, en)
		case StatePending:
			if en.OutOfOrder && !opts.AllowOutOfOrder {
				order.Entries = append(order.Entries, en)
			}
			pending = append(pending, en.File)
		}
	}
	if len(drift.Modified) > 0 || len(drift.Missing) > 0 {
		return nil, &drift
	}
	if len(order.Entries) > 0 {
		return nil, &order
	}
	if opts.Target != "" {
		var cut []*Migration
		found := false
		for _, m := range pending {
			cut = append(cut, m)
			if m.Version == opts.Target {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("target %s is not a pending migration", opts.Target)
		}
		pending = cut
	}
	for _, m := range pending {
		if m.Empty {
			return nil, &EmptyError{Migration: m}
		}
	}
	return pending, nil
}

// Apply runs pending migrations. Each migration runs in its own transaction
// (unless marked no-transaction) under a session advisory lock. With DryRun,
// everything runs in one transaction that is always rolled back.
func (e *Engine) Apply(ctx context.Context, opts ApplyOptions, obs Observer) (*ApplyResult, error) {
	if obs == nil {
		obs = nopObserver{}
	}
	conn, err := e.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	unlock, err := lock(ctx, conn, e.lockTimeout())
	if err != nil {
		return nil, err
	}
	defer unlock()

	files, err := LoadDir(e.Dir)
	if err != nil {
		return nil, err
	}
	records, err := Applied(ctx, conn)
	if err != nil {
		return nil, err
	}
	plan, err := e.Plan(merge(files, records), opts)
	if err != nil {
		return nil, err
	}
	res := &ApplyResult{DryRun: opts.DryRun}
	if len(plan) == 0 {
		return res, nil
	}
	if opts.DryRun {
		return e.preview(ctx, conn.Conn(), plan, res, obs)
	}

	var before *introspect.Snapshot
	if !opts.SkipDiff {
		if before, err = introspect.Load(ctx, conn, e.Schemas); err != nil {
			return nil, err
		}
	}
	if err := ensureHistory(ctx, conn); err != nil {
		return nil, err
	}
	for _, m := range plan {
		obs.Started(m)
		start := time.Now()
		err := e.applyOne(ctx, conn.Conn(), m)
		d := time.Since(start)
		obs.Finished(m, d, err)
		if err != nil {
			return res, err
		}
		res.Applied = append(res.Applied, AppliedMigration{Migration: m, Duration: d})
	}
	if before != nil {
		after, err := introspect.Load(ctx, conn, e.Schemas)
		if err != nil {
			return res, err
		}
		d := schemadiff.Compare(before, after)
		res.Diff = &d
	}
	return res, nil
}

func (e *Engine) applyOne(ctx context.Context, conn *pgx.Conn, m *Migration) error {
	if m.NoTransaction {
		start := time.Now()
		for _, st := range sqltext.Split(m.Up) {
			if _, err := conn.Exec(ctx, st.SQL); err != nil {
				return &Error{Migration: m, Section: "up", SQL: st.SQL, LineOffset: m.UpLine - 1 + st.Line - 1, Err: err}
			}
		}
		return recordApply(ctx, conn, m, time.Since(start), e.NexusVersion)
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		start := time.Now()
		if _, err := tx.Exec(ctx, m.Up); err != nil {
			return &Error{Migration: m, Section: "up", SQL: m.Up, LineOffset: m.UpLine - 1, Err: err}
		}
		return recordApply(ctx, tx, m, time.Since(start), e.NexusVersion)
	})
}

// errRollbackPreview unwinds the preview transaction.
var errRollbackPreview = errors.New("preview complete")

func (e *Engine) preview(ctx context.Context, conn *pgx.Conn, plan []*Migration, res *ApplyResult, obs Observer) (*ApplyResult, error) {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		before, err := introspect.Load(ctx, tx, e.Schemas)
		if err != nil {
			return err
		}
		for _, m := range plan {
			if m.NoTransaction {
				res.Applied = append(res.Applied, AppliedMigration{Migration: m, Skipped: "no-transaction migrations can't run inside a preview"})
				continue
			}
			obs.Started(m)
			start := time.Now()
			_, err := tx.Exec(ctx, m.Up)
			d := time.Since(start)
			if err != nil {
				err = &Error{Migration: m, Section: "up", SQL: m.Up, LineOffset: m.UpLine - 1, Err: err}
			}
			obs.Finished(m, d, err)
			if err != nil {
				return err
			}
			res.Applied = append(res.Applied, AppliedMigration{Migration: m, Duration: d})
		}
		after, err := introspect.Load(ctx, tx, e.Schemas)
		if err != nil {
			return err
		}
		diff := schemadiff.Compare(before, after)
		res.Diff = &diff
		return errRollbackPreview
	})
	if errors.Is(err, errRollbackPreview) {
		return res, nil
	}
	return res, err
}

// RollbackResult reports reverted migrations.
type RollbackResult struct {
	Reverted []AppliedMigration `json:"reverted"`
	Diff     *schemadiff.Diff   `json:"diff,omitempty"`
}

// IrreversibleError means a migration to roll back has no down section or no file.
type IrreversibleError struct {
	Entry Entry
}

func (e *IrreversibleError) Error() string {
	if e.Entry.File == nil {
		return fmt.Sprintf("migration %s_%s can't be rolled back: its file is missing", e.Entry.Version, e.Entry.Name)
	}
	return fmt.Sprintf("migration %s can't be rolled back: it has no -- nexus:down section", e.Entry.File.ID())
}

// RollbackPlan returns the applied entries the last `steps` rollbacks would revert,
// most recent first.
func RollbackPlan(st *Status, steps int) ([]Entry, error) {
	var applied []Entry
	for _, en := range st.Entries {
		if en.AppliedAt != nil {
			applied = append(applied, en)
		}
	}
	sort.SliceStable(applied, func(i, j int) bool {
		if !applied[i].AppliedAt.Equal(*applied[j].AppliedAt) {
			return applied[i].AppliedAt.After(*applied[j].AppliedAt)
		}
		return versionLess(applied[j].Version, applied[i].Version)
	})
	if steps > len(applied) {
		steps = len(applied)
	}
	plan := applied[:steps]
	for _, en := range plan {
		if en.File == nil || !en.File.HasDown {
			return nil, &IrreversibleError{Entry: en}
		}
	}
	return plan, nil
}

// Rollback reverts the most recently applied migrations, each in its own
// transaction.
func (e *Engine) Rollback(ctx context.Context, steps int, obs Observer) (*RollbackResult, error) {
	if obs == nil {
		obs = nopObserver{}
	}
	conn, err := e.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	unlock, err := lock(ctx, conn, e.lockTimeout())
	if err != nil {
		return nil, err
	}
	defer unlock()

	files, err := LoadDir(e.Dir)
	if err != nil {
		return nil, err
	}
	records, err := Applied(ctx, conn)
	if err != nil {
		return nil, err
	}
	plan, err := RollbackPlan(merge(files, records), steps)
	if err != nil {
		return nil, err
	}
	res := &RollbackResult{}
	if len(plan) == 0 {
		return res, nil
	}
	before, err := introspect.Load(ctx, conn, e.Schemas)
	if err != nil {
		return nil, err
	}
	for _, en := range plan {
		m := en.File
		obs.Started(m)
		start := time.Now()
		err := e.revertOne(ctx, conn.Conn(), m, start)
		d := time.Since(start)
		obs.Finished(m, d, err)
		if err != nil {
			return res, err
		}
		res.Reverted = append(res.Reverted, AppliedMigration{Migration: m, Duration: d})
	}
	after, err := introspect.Load(ctx, conn, e.Schemas)
	if err != nil {
		return res, err
	}
	diff := schemadiff.Compare(before, after)
	res.Diff = &diff
	return res, nil
}

// revertOne runs a migration's down section: in a transaction, or statement
// by statement for no-transaction migrations (DROP INDEX CONCURRENTLY…).
func (e *Engine) revertOne(ctx context.Context, conn *pgx.Conn, m *Migration, start time.Time) error {
	if m.NoTransaction {
		for _, st := range sqltext.Split(m.Down) {
			if _, err := conn.Exec(ctx, st.SQL); err != nil {
				return &Error{Migration: m, Section: "down", SQL: st.SQL, LineOffset: m.DownLine - 1 + st.Line - 1, Err: err}
			}
		}
		return recordRollback(ctx, conn, m.Version, m.Name, time.Since(start))
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, m.Down); err != nil {
			return &Error{Migration: m, Section: "down", SQL: m.Down, LineOffset: m.DownLine - 1, Err: err}
		}
		return recordRollback(ctx, tx, m.Version, m.Name, time.Since(start))
	})
}

// RepairAction is one fix Repair would make.
type RepairAction struct {
	Entry  Entry  `json:"migration"`
	Action string `json:"action"` // "accept-checksum" | "forget"
}

// RepairPlan lists the history fixes needed to reconcile drift: accept the
// current checksum of edited files, and forget history rows whose files are gone.
func RepairPlan(st *Status) []RepairAction {
	var out []RepairAction
	for _, en := range st.Entries {
		switch en.State {
		case StateModified:
			out = append(out, RepairAction{Entry: en, Action: "accept-checksum"})
		case StateMissing:
			out = append(out, RepairAction{Entry: en, Action: "forget"})
		}
	}
	return out
}

// Repair applies a repair plan in one transaction. It changes only Nexus's
// history, never your schema.
func (e *Engine) Repair(ctx context.Context, plan []RepairAction) error {
	conn, err := e.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	unlock, err := lock(ctx, conn, e.lockTimeout())
	if err != nil {
		return err
	}
	defer unlock()
	return pgx.BeginFunc(ctx, conn.Conn(), func(tx pgx.Tx) error {
		for _, a := range plan {
			switch a.Action {
			case "accept-checksum":
				if _, err := tx.Exec(ctx, `update nexus_meta.migrations set checksum = $2 where version = $1`, a.Entry.Version, a.Entry.File.Checksum); err != nil {
					return err
				}
			case "forget":
				if _, err := tx.Exec(ctx, `delete from nexus_meta.migrations where version = $1`, a.Entry.Version); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown repair action %q", a.Action)
			}
			if err := recordEvent(ctx, tx, a.Entry.Version, a.Entry.Name, "repair", nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *Engine) lockTimeout() time.Duration {
	if e.LockTimeout > 0 {
		return e.LockTimeout
	}
	return 15 * time.Second
}

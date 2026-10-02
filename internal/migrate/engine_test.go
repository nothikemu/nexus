package migrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pgtest"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEngineLifecycle(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	ctx := context.Background()
	dir := t.TempDir()
	e := &Engine{Pool: pool, Dir: dir, Schemas: []string{"public"}, NexusVersion: "test"}

	write(t, dir, "20260101000000_users.sql", "-- nexus:up\ncreate table users (id int primary key, email text);\n-- nexus:down\ndrop table users;\n")
	write(t, dir, "20260102000000_index.sql", "-- nexus:up\ncreate index users_email_idx on users (email);\n-- nexus:down\ndrop index users_email_idx;\n")

	st, err := e.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending != 2 || st.Applied != 0 {
		t.Fatalf("status = %+v", st)
	}
	// Status is read-only: it must not create Nexus's tables.
	var exists bool
	_ = pool.QueryRow(ctx, `select to_regclass('nexus_meta.migrations') is not null`).Scan(&exists)
	if exists {
		t.Fatal("Status created the history table")
	}

	// Dry run applies nothing but reports the diff.
	res, err := e.Apply(ctx, ApplyOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || res.Diff == nil || res.Diff.TablesTouched() != 1 {
		t.Fatalf("dry run = %+v diff=%+v", res, res.Diff)
	}
	if st, _ := e.Status(ctx); st.Pending != 2 {
		t.Fatal("dry run persisted changes")
	}

	res, err = e.Apply(ctx, ApplyOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || res.Diff.Count("added") == 0 {
		t.Fatalf("apply = %+v diff=%+v", res, res.Diff)
	}
	st, _ = e.Status(ctx)
	if !st.UpToDate() || st.Applied != 2 {
		t.Fatalf("after apply = %+v", st)
	}

	// Editing an applied migration is drift; apply refuses, repair accepts.
	write(t, dir, "20260101000000_users.sql", "-- nexus:up\ncreate table users (id int primary key, email text, name text);\n-- nexus:down\ndrop table users;\n")
	write(t, dir, "20260103000000_more.sql", "-- nexus:up\nalter table users add column bio text;\n-- nexus:down\nalter table users drop column bio;\n")
	var drift *DriftError
	if _, err := e.Apply(ctx, ApplyOptions{}, nil); !errors.As(err, &drift) || len(drift.Modified) != 1 {
		t.Fatalf("expected drift error, got %v", err)
	}
	st, _ = e.Status(ctx)
	plan := RepairPlan(st)
	if len(plan) != 1 || plan[0].Action != "accept-checksum" {
		t.Fatalf("repair plan = %+v", plan)
	}
	if err := e.Repair(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, ApplyOptions{}, nil); err != nil {
		t.Fatal(err)
	}

	// Rollback the two most recent migrations.
	rb, err := e.Rollback(ctx, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.Reverted) != 2 || rb.Reverted[0].Migration.Name != "more" {
		t.Fatalf("rollback = %+v", rb.Reverted)
	}
	st, _ = e.Status(ctx)
	if st.Pending != 2 || st.Applied != 1 {
		t.Fatalf("after rollback = %+v", st)
	}

	events, err := RecentEvents(ctx, pool, 20)
	if err != nil || len(events) < 6 {
		t.Fatalf("events = %d, err = %v", len(events), err)
	}
}

func TestEngineReportsErrorPosition(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	ctx := context.Background()
	dir := t.TempDir()
	e := &Engine{Pool: pool, Dir: dir, Schemas: []string{"public"}, NexusVersion: "test"}
	write(t, dir, "20260101000000_bad.sql", "-- a comment\n-- nexus:up\ncreate table ok (id int);\ncreate table bad (id integr);\n")

	_, err := e.Apply(ctx, ApplyOptions{}, nil)
	var me *Error
	if !errors.As(err, &me) {
		t.Fatalf("expected migrate.Error, got %v", err)
	}
	pe, ok := pg.AsPgError(err)
	if !ok || pe.Code != "42704" || pe.Position == 0 {
		t.Fatalf("pg error = %+v", pe)
	}
	if me.LineOffset != 2 {
		t.Errorf("line offset = %d, want 2", me.LineOffset)
	}
	// The failed migration rolled back completely.
	var exists bool
	_ = pool.QueryRow(ctx, `select to_regclass('public.ok') is not null`).Scan(&exists)
	if exists {
		t.Error("partial migration was committed")
	}
}

func TestEngineOutOfOrder(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	ctx := context.Background()
	dir := t.TempDir()
	e := &Engine{Pool: pool, Dir: dir, Schemas: []string{"public"}, NexusVersion: "test"}
	write(t, dir, "20260102000000_b.sql", "create table b (id int);")
	if _, err := e.Apply(ctx, ApplyOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "20260101000000_a.sql", "create table a (id int);")
	var oe *OrderError
	if _, err := e.Apply(ctx, ApplyOptions{}, nil); !errors.As(err, &oe) {
		t.Fatalf("expected OrderError, got %v", err)
	}
	if _, err := e.Apply(ctx, ApplyOptions{AllowOutOfOrder: true}, nil); err != nil {
		t.Fatal(err)
	}
}

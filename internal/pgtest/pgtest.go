// Package pgtest provides isolated PostgreSQL databases for integration tests.
//
// Tests that need a real server call pgtest.URL(t). It reads
// NEXUS_TEST_DATABASE_URL (a role that may CREATE DATABASE), creates a fresh
// database for the test and drops it afterwards. Without the variable the
// test is skipped with an explanation, so `go test ./...` stays green on
// machines without PostgreSQL.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvVar names the base connection string for integration tests.
const EnvVar = "NEXUS_TEST_DATABASE_URL"

// URL returns the connection string of a fresh, empty database.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvVar)
	if base == "" {
		t.Skipf("integration test: set %s to a PostgreSQL URL to run it", EnvVar)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("pgtest: connect %s: %v", EnvVar, err)
	}
	defer admin.Close(ctx)

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "nexus_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Logf("pgtest: cleanup connect: %v", err)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "drop database if exists "+pgx.Identifier{name}.Sanitize()+" with (force)"); err != nil {
			t.Logf("pgtest: drop database %s: %v", name, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("pgtest: %s must be a URL: %v", EnvVar, err)
	}
	u.Path = "/" + name
	return u.String()
}

// Pool opens a pool on a fresh database and closes it at cleanup.
func Pool(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	u := URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, u)
	if err != nil {
		t.Fatalf("pgtest: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, u
}

// Exec runs setup SQL, failing the test on error.
func Exec(t testing.TB, pool *pgxpool.Pool, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("pgtest: exec: %v\n%s", err, sql)
	}
}

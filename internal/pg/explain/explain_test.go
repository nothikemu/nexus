package explain

import (
	"context"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pgtest"
)

const seqScanPlan = `[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "users", "Schema": "public", "Alias": "users",
 "Startup Cost": 0, "Total Cost": 3891.64, "Plan Rows": 1, "Plan Width": 64,
 "Actual Startup Time": 0.02, "Actual Total Time": 12.3, "Actual Rows": 1, "Actual Loops": 1,
 "Filter": "(email = 'x'::text)", "Rows Removed by Filter": 184290},
 "Planning Time": 0.1, "Execution Time": 12.4}]`

func usersTable(indexes ...*introspect.Index) *introspect.Snapshot {
	return &introspect.Snapshot{Schemas: []string{"public"}, Tables: []*introspect.Table{{
		Schema: "public", Name: "users", Kind: introspect.KindTable, Rows: 184291,
		Columns: []*introspect.Column{{Name: "id"}, {Name: "email"}},
		Indexes: indexes,
	}}}
}

func TestAnalyzeSuggestsIndexForRealColumn(t *testing.T) {
	p, err := Parse([]byte(seqScanPlan), true)
	if err != nil {
		t.Fatal(err)
	}
	tot := p.Totals()
	if tot.RowsScanned != 184291 || tot.RowsReturned != 1 || tot.SeqScans != 1 {
		t.Fatalf("totals = %+v", tot)
	}
	ins := Analyze(p, usersTable())
	if len(ins) == 0 || ins[0].Severity != Warning || !strings.Contains(ins[0].SQL, `create index concurrently users_email_idx on "public"."users" ("email")`) {
		t.Fatalf("insights = %+v", ins)
	}
}

func TestAnalyzeExistingIndexSuggestsAnalyze(t *testing.T) {
	p, _ := Parse([]byte(seqScanPlan), true)
	ins := Analyze(p, usersTable(&introspect.Index{Name: "users_email_key", Columns: []string{"email"}, Valid: true}))
	if !strings.HasPrefix(ins[0].SQL, "analyze") {
		t.Fatalf("insights = %+v", ins)
	}
}

func TestAnalyzeNeverInventsColumns(t *testing.T) {
	plan := strings.Replace(seqScanPlan, "(email = 'x'::text)", "(nickname = 'x'::text)", 1)
	p, _ := Parse([]byte(plan), true)
	for _, in := range Analyze(p, usersTable()) {
		if strings.Contains(in.SQL, "nickname") {
			t.Fatalf("suggested an index on a column that doesn't exist: %+v", in)
		}
	}
	// Without a snapshot, no SQL is suggested at all.
	for _, in := range Analyze(p, nil) {
		if strings.Contains(in.SQL, "create index") {
			t.Fatalf("suggested an index without catalog knowledge: %+v", in)
		}
	}
}

func TestAnalyzeLowerExpression(t *testing.T) {
	plan := strings.Replace(seqScanPlan, "(email = 'x'::text)", "(lower(email) = 'x'::text)", 1)
	p, _ := Parse([]byte(plan), true)
	ins := Analyze(p, usersTable())
	if !strings.Contains(ins[0].SQL, `(lower("email"))`) {
		t.Fatalf("insights = %+v", ins)
	}
}

func TestLabel(t *testing.T) {
	cases := map[string]*Node{
		"Seq Scan on users u":                  {Type: "Seq Scan", Relation: "users", Alias: "u"},
		"Hash Left Join":                       {Type: "Hash Join", JoinType: "Left"},
		"Index Scan using users_pkey on users": {Type: "Index Scan", Index: "users_pkey", Relation: "users", Alias: "users"},
		"Nested Loop Anti Join":                {Type: "Nested Loop", JoinType: "Anti"},
	}
	for want, n := range cases {
		if got := n.Label(); got != want {
			t.Errorf("Label = %q, want %q", got, want)
		}
	}
}

func TestRunAgainstPostgres(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	pgtest.Exec(t, pool, `create table users (id int primary key, email text);
		insert into users select g, 'user' || g || '@example.com' from generate_series(1, 20000) g;
		analyze users;`)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	p, err := Run(ctx, conn.Conn(), "select * from users where email = 'user5@example.com';", true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Analyzed || p.Root.Type != "Seq Scan" || p.ExecutionMs <= 0 {
		t.Fatalf("plan = %+v", p.Root)
	}
	snap, err := introspect.Load(ctx, pool, []string{"public"})
	if err != nil {
		t.Fatal(err)
	}
	ins := Analyze(p, snap)
	if !strings.Contains(ins[0].SQL, "users_email_idx") {
		t.Fatalf("insights = %+v", ins)
	}

	// ANALYZE of a write is rolled back.
	if _, err := Run(ctx, conn.Conn(), "delete from users", true); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pool.QueryRow(ctx, "select count(*) from users").Scan(&n)
	if n != 20000 {
		t.Fatalf("explain analyze persisted a delete: %d rows left", n)
	}
	if _, err := Run(ctx, conn.Conn(), "select 1; select 2", false); err != ErrMultipleStatements {
		t.Fatalf("err = %v", err)
	}
}

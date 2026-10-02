package rows

import (
	"context"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pgtest"
)

func users() *introspect.Table {
	return &introspect.Table{Schema: "public", Name: "users", PrimaryKey: []string{"id"},
		Columns: []*introspect.Column{{Name: "id"}, {Name: "email"}, {Name: "created_at"}}}
}

func TestQuerySQL(t *testing.T) {
	sql, args := Query{Table: users(), Search: "50%_off", Where: "id > 3", Limit: 20, Offset: 40}.SQL()
	want := `select * from "public"."users" where (id > 3) and ("id"::text ilike $1 or "email"::text ilike $1 or "created_at"::text ilike $1) order by "id" limit 20 offset 40`
	if sql != want {
		t.Errorf("sql =\n%s\nwant\n%s", sql, want)
	}
	if len(args) != 1 || args[0] != `%50\%\_off%` {
		t.Errorf("args = %v (LIKE wildcards in the search must be escaped)", args)
	}
	count, _ := Query{Table: users()}.CountSQL(101)
	if count != `select count(*) from (select 1 from "public"."users" limit 101) s` {
		t.Errorf("count = %s", count)
	}
}

func TestParseOrder(t *testing.T) {
	keys, err := ParseOrder(users(), "created_at desc, email")
	if err != nil || len(keys) != 2 || !keys[0].Desc || keys[1].Column != "email" {
		t.Fatalf("keys = %+v, err = %v", keys, err)
	}
	if _, err := ParseOrder(users(), "nope"); err == nil {
		t.Error("unknown column accepted")
	}
	if _, err := ParseOrder(users(), "email; drop table users"); err == nil {
		t.Error("injection accepted")
	}
}

func TestFetchIsReadOnly(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	pgtest.Exec(t, pool, `create table public.users (id int primary key, email text, created_at timestamptz default now());
		insert into users (id, email) select g, 'u' || g || '@x.io' from generate_series(1, 30) g;
		create function sneaky() returns boolean language sql as $$ insert into users (id) values (999) returning true $$;`)
	ctx := context.Background()
	res, total, err := Fetch(ctx, pool, Query{Table: users(), Search: "u1", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if total != 11 || len(res.Rows) != 5 || res.Rows[0][1].Text != "u1@x.io" {
		t.Fatalf("total=%d rows=%v", total, res.Rows)
	}
	// A filter that tries to write is refused by the read-only transaction.
	_, _, err = Fetch(ctx, pool, Query{Table: users(), Where: "sneaky()", Limit: 5})
	if err == nil || !strings.Contains(err.Error(), "read-only transaction") {
		t.Fatalf("expected a read-only error, got %v", err)
	}
	var n int
	_ = pool.QueryRow(ctx, "select count(*) from users").Scan(&n)
	if n != 30 {
		t.Fatalf("rows were modified: %d", n)
	}
}

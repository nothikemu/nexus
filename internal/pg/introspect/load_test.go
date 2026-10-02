package introspect_test

import (
	"context"
	"testing"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pgtest"
)

const fixture = `
create type public.plan as enum ('free', 'pro', 'team');

create table public.orgs (
  id   bigint generated always as identity primary key,
  name text not null,
  plan public.plan not null default 'free'
);
comment on table public.orgs is 'Organizations';

create table public.users (
  id         uuid primary key default gen_random_uuid(),
  org_id     bigint not null references public.orgs (id) on delete cascade,
  email      text not null unique,
  name       text,
  name_lower text generated always as (lower(name)) stored,
  created_at timestamptz not null default now(),
  constraint email_has_at check (position('@' in email) > 1)
);
create index users_created_at_idx on public.users (created_at desc) where name is not null;

alter table public.users enable row level security;
create policy users_self on public.users for select to public using (true);

create view public.active_users as select id, email from public.users where name is not null;

create function public.touch() returns trigger language plpgsql as $$
begin new.created_at := now(); return new; end $$;
create trigger users_touch before update on public.users for each row execute function public.touch();

create table public.events (id bigint, at timestamptz not null) partition by range (at);
create table public.events_2026 partition of public.events for values from ('2026-01-01') to ('2027-01-01');
`

func TestLoad(t *testing.T) {
	pool, _ := pgtest.Pool(t)
	pgtest.Exec(t, pool, fixture)

	snap, err := introspect.Load(context.Background(), pool, []string{"public"})
	if err != nil {
		t.Fatal(err)
	}

	users := snap.Table("users")
	if users == nil {
		t.Fatal("users not found")
	}
	if got := len(users.Columns); got != 6 {
		t.Fatalf("users columns = %d, want 6", got)
	}
	if users.Column("name_lower").Generated == "" {
		t.Error("name_lower should be generated")
	}
	if users.Column("id").Default != "gen_random_uuid()" {
		t.Errorf("id default = %q", users.Column("id").Default)
	}
	if len(users.PrimaryKey) != 1 || users.PrimaryKey[0] != "id" {
		t.Errorf("primary key = %v", users.PrimaryKey)
	}
	if !users.IsUnique("email") {
		t.Error("email should be unique")
	}
	if fk := users.ForeignKeyFor("org_id"); fk == nil || fk.RefTable != "orgs" || fk.OnDelete != "cascade" {
		t.Errorf("org_id foreign key = %+v", fk)
	}
	if !users.RLSEnabled || len(users.Policies) != 1 || users.Policies[0].Command != "SELECT" {
		t.Errorf("rls=%v policies=%+v", users.RLSEnabled, users.Policies)
	}
	if len(users.Triggers) != 1 {
		t.Errorf("triggers = %d", len(users.Triggers))
	}
	var partial *introspect.Index
	for _, ix := range users.Indexes {
		if ix.Name == "users_created_at_idx" {
			partial = ix
		}
	}
	if partial == nil || partial.Predicate == "" || partial.Columns[0] != "created_at" {
		t.Errorf("partial index = %+v", partial)
	}
	var hasCheck bool
	for _, c := range users.Constraints {
		hasCheck = hasCheck || (c.Type == "check" && c.Name == "email_has_at")
	}
	if !hasCheck {
		t.Error("check constraint missing")
	}

	orgs := snap.Table("public.orgs")
	if orgs.Comment != "Organizations" || orgs.Column("id").Identity != "always" {
		t.Errorf("orgs = %+v", orgs)
	}
	if len(orgs.ReferencedBy) != 1 || orgs.ReferencedBy[0].Table != "users" {
		t.Errorf("orgs referenced by = %+v", orgs.ReferencedBy)
	}
	if !orgs.Column("plan").IsEnum {
		t.Error("plan should be an enum column")
	}

	if v := snap.Table("active_users"); v == nil || v.Kind != introspect.KindView || v.Definition == "" {
		t.Errorf("view = %+v", v)
	}
	if p := snap.Table("events"); p == nil || p.Kind != introspect.KindPartitioned {
		t.Errorf("partitioned = %+v", p)
	}
	if c := snap.Table("events_2026"); c == nil || c.PartitionOf != "public.events" {
		t.Errorf("partition = %+v", c)
	}
	if len(snap.BaseTables()) != 3 {
		t.Errorf("base tables = %d, want 3 (orgs, users, events)", len(snap.BaseTables()))
	}
	if len(snap.Enums) != 1 || len(snap.Enums[0].Values) != 3 {
		t.Errorf("enums = %+v", snap.Enums)
	}
	if len(snap.Functions) != 1 || snap.Functions[0].Name != "touch" {
		t.Errorf("functions = %+v", snap.Functions)
	}
	if len(snap.Sequences) != 1 || snap.Sequences[0].OwnedBy != "public.orgs.id" {
		t.Errorf("sequences = %+v", snap.Sequences)
	}
	if got := snap.Suggest("user"); len(got) == 0 {
		t.Error("expected suggestions for 'user'")
	}
}

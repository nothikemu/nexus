package schemadiff

import (
	"testing"

	"github.com/nothikemu/nexus/internal/pg/introspect"
)

func snap(tables ...*introspect.Table) *introspect.Snapshot {
	return &introspect.Snapshot{Schemas: []string{"public"}, Tables: tables}
}

func users(cols ...*introspect.Column) *introspect.Table {
	return &introspect.Table{Schema: "public", Name: "users", Kind: introspect.KindTable, Columns: cols}
}

func col(name, typ string, nullable bool) *introspect.Column {
	return &introspect.Column{Name: name, Type: typ, Nullable: nullable}
}

func TestCompareNoChanges(t *testing.T) {
	a := snap(users(col("id", "uuid", false)))
	b := snap(users(col("id", "uuid", false)))
	if d := Compare(a, b); !d.Empty() {
		t.Fatalf("expected no changes, got %+v", d.Changes)
	}
}

func TestCompareTableAndColumns(t *testing.T) {
	a := snap(users(col("id", "uuid", false), col("name", "text", true), col("legacy", "text", true)))
	b := snap(
		users(col("id", "uuid", false), col("name", "varchar(80)", false), col("bio", "text", true)),
		&introspect.Table{Schema: "public", Name: "profiles", Kind: introspect.KindTable, Columns: []*introspect.Column{col("id", "uuid", false)}},
	)
	d := Compare(a, b)

	want := map[string]Kind{
		"public.profiles": Added,
		"bio":             Added,
		"name":            Changed,
		"legacy":          Removed,
	}
	got := map[string]Kind{}
	for _, c := range d.Changes {
		got[c.Name] = c.Kind
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: got %q, want %q (all: %+v)", name, got[name], kind, d.Changes)
		}
	}
	for _, c := range d.Changes {
		if c.Name == "name" && c.Detail != "type text → varchar(80), nullable → not null" {
			t.Errorf("name detail = %q", c.Detail)
		}
	}
	if d.TablesTouched() != 2 {
		t.Errorf("tables touched = %d, want 2", d.TablesTouched())
	}
	// Table-level changes sort before their sub-objects, grouped by table.
	if d.Changes[0].Name != "public.profiles" {
		t.Errorf("first change = %+v", d.Changes[0])
	}
}

func TestCompareIndexesPoliciesRLS(t *testing.T) {
	a := users(col("id", "uuid", false))
	b := users(col("id", "uuid", false))
	b.Indexes = []*introspect.Index{{Name: "users_email_idx", Definition: "CREATE INDEX users_email_idx ON public.users (email)"}}
	b.RLSEnabled = true
	b.Policies = []*introspect.Policy{{Name: "own", Command: "SELECT", Using: "true"}}
	d := Compare(snap(a), snap(b))
	if d.Count(Added) != 2 || d.Count(Changed) != 1 {
		t.Fatalf("changes = %+v", d.Changes)
	}
}

func TestCompareEnumsAndFunctions(t *testing.T) {
	a := &introspect.Snapshot{
		Enums:     []*introspect.Enum{{Schema: "public", Name: "plan", Values: []string{"free"}}},
		Functions: []*introspect.Function{{Schema: "public", Name: "f", Kind: "function", BodyHash: "a"}},
	}
	b := &introspect.Snapshot{
		Enums:     []*introspect.Enum{{Schema: "public", Name: "plan", Values: []string{"free", "pro"}}},
		Functions: []*introspect.Function{{Schema: "public", Name: "f", Kind: "function", BodyHash: "b"}},
	}
	d := Compare(a, b)
	if d.Count(Changed) != 2 {
		t.Fatalf("changes = %+v", d.Changes)
	}
}

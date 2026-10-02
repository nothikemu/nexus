package safety

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/ui"
)

func guard(input string, interactive bool) (*Guard, *bytes.Buffer) {
	var out, errb bytes.Buffer
	m := ui.PlainMode()
	m.Interactive = interactive
	p := ui.NewTestPrinter(m, &out, &errb)
	p.In = strings.NewReader(input)
	return &Guard{P: p}, &errb
}

func TestReadOnlyAlwaysAllowed(t *testing.T) {
	g, _ := guard("", false)
	if err := g.Allow(Operation{Level: ReadOnly}, Target{Env: "production", Protected: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDestructiveLocal(t *testing.T) {
	g, _ := guard("y\n", true)
	if err := g.Allow(Operation{Level: Destructive, Action: "reset"}, Target{Env: "local"}); err != nil {
		t.Fatalf("yes answer: %v", err)
	}
	g, _ = guard("n\n", true)
	if err := g.Allow(Operation{Level: Destructive}, Target{Env: "local"}); !errors.Is(err, ErrDeclined) {
		t.Fatalf("no answer: %v", err)
	}
	g, _ = guard("", false)
	var cre *ConfirmationRequiredError
	if err := g.Allow(Operation{Level: Destructive}, Target{Env: "local"}); !errors.As(err, &cre) || cre.Phrase != "" {
		t.Fatalf("non-interactive: %v", err)
	}
	g.Yes = true
	if err := g.Allow(Operation{Level: Destructive}, Target{Env: "local"}); err != nil {
		t.Fatalf("--yes: %v", err)
	}
}

func TestDestructiveProtectedNeedsPhrase(t *testing.T) {
	prod := Target{Env: "production", Protected: true}
	g, errb := guard("yes\n", true)
	if err := g.Allow(Operation{Level: Destructive, Action: "roll back"}, prod); !errors.Is(err, ErrDeclined) {
		t.Fatalf("'yes' must not satisfy a protected confirmation: %v", err)
	}
	if !strings.Contains(errb.String(), "DELETE PRODUCTION DATA") || !strings.Contains(errb.String(), "permanently delete data") {
		t.Errorf("warning/prompt missing:\n%s", errb.String())
	}
	g, _ = guard("DELETE PRODUCTION DATA\n", true)
	if err := g.Allow(Operation{Level: Destructive}, prod); err != nil {
		t.Fatalf("typed phrase: %v", err)
	}
	// --yes alone is never enough for a protected environment.
	g, _ = guard("", false)
	g.Yes = true
	var cre *ConfirmationRequiredError
	if err := g.Allow(Operation{Level: Destructive}, prod); !errors.As(err, &cre) || cre.Phrase != "DELETE PRODUCTION DATA" {
		t.Fatalf("--yes on production: %v", err)
	}
	g.Confirm = "DELETE PRODUCTION DATA"
	if err := g.Allow(Operation{Level: Destructive}, prod); err != nil {
		t.Fatalf("--confirm: %v", err)
	}
	g.Confirm = "delete production data"
	if err := g.Allow(Operation{Level: Destructive}, prod); err == nil {
		t.Fatal("wrong phrase accepted")
	}
}

func TestSafeWriteOnProtectedAsks(t *testing.T) {
	g, _ := guard("n\n", true)
	if err := g.Allow(Operation{Level: SafeWrite, Action: "apply 2 migrations"}, Target{Env: "production", Protected: true}); !errors.Is(err, ErrDeclined) {
		t.Fatalf("got %v", err)
	}
	g, _ = guard("", false)
	if err := g.Allow(Operation{Level: SafeWrite}, Target{Env: "staging"}); err != nil {
		t.Fatalf("unprotected safe write: %v", err)
	}
}

func TestClassifySQL(t *testing.T) {
	cases := map[string]Level{
		"select * from users":                               ReadOnly,
		"explain select 1":                                  ReadOnly,
		"explain analyze delete from users":                 Destructive,
		"with x as (select 1) select * from x":              ReadOnly,
		"with gone as (delete from t returning *) select 1": Destructive,
		"insert into t values (1)":                          SafeWrite,
		"update t set a = 1 where id = 2":                   SafeWrite,
		"update t set a = 1":                                Destructive,
		"delete from t where id = 1":                        Destructive,
		"drop table t":                                      Destructive,
		"alter table t add column b int":                    SafeWrite,
		"alter table t drop column b":                       Destructive,
		"select 1; truncate t":                              Destructive,
		"-- just a comment\nselect 1":                       ReadOnly,
		"create table t (id int)":                           SafeWrite,
	}
	for sql, want := range cases {
		if got, _ := ClassifySQL(sql); got != want {
			t.Errorf("ClassifySQL(%q) = %v, want %v", sql, got, want)
		}
	}
}

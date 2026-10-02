package config

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	c, warnings, err := Parse([]byte("project:\n  name: my-app\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if c.Database.Name != "my_app" || c.Database.Port != DefaultPort || c.Database.Version != DefaultPostgresVersion ||
		c.Database.Runtime != RuntimeAuto || c.Migrations.Dir != "migrations" || c.Database.Schemas[0] != "public" {
		t.Errorf("defaults = %+v", c.Database)
	}
	if !c.Dev.ShouldAutoMigrate() || !c.Dev.ShouldAutoSeed() {
		t.Error("dev defaults should be on")
	}
}

func TestParseURLImpliesExternal(t *testing.T) {
	c, _, err := Parse([]byte("project:\n  name: a\ndatabase:\n  url: ${DATABASE_URL}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Runtime != RuntimeExternal {
		t.Errorf("runtime = %s", c.Database.Runtime)
	}
}

func TestUnknownKeysSuggest(t *testing.T) {
	_, warnings, err := Parse([]byte("project:\n  name: a\ndatabse:\n  port: 5555\nmigrations:\n  dirr: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{`unknown key "databse" (did you mean "database"?)`, `unknown key "migrations.dirr" (did you mean "migrations.dir"?)`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]string{
		"name":     "project:\n  name: My App\n",
		"runtime":  "project:\n  name: a\ndatabase:\n  runtime: podman\n",
		"version":  "project:\n  name: a\ndatabase:\n  version: 9\n",
		"port":     "project:\n  name: a\ndatabase:\n  port: 80\n",
		"external": "project:\n  name: a\ndatabase:\n  runtime: external\n",
		"local":    "project:\n  name: a\nenvironments:\n  local:\n    database_url: x\n",
		"envurl":   "project:\n  name: a\nenvironments:\n  staging: {}\n",
		"dir":      "project:\n  name: a\nmigrations:\n  dir: ../elsewhere\n",
		"yaml":     "project: [unclosed\n",
	}
	for name, src := range bad {
		var ce *Error
		if _, _, err := Parse([]byte(src)); !errors.As(err, &ce) {
			t.Errorf("%s: expected a config error, got %v", name, err)
		}
	}
}

func TestProtectedDefaults(t *testing.T) {
	yes := true
	no := false
	cases := []struct {
		name string
		env  Environment
		want bool
	}{
		{"production", Environment{}, true},
		{"prod", Environment{}, true},
		{"staging", Environment{}, false},
		{"staging", Environment{Protected: &yes}, true},
		{"production", Environment{Protected: &no}, false},
	}
	for _, c := range cases {
		if got := c.env.IsProtected(c.name); got != c.want {
			t.Errorf("%s protected = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInterpolate(t *testing.T) {
	env := map[string]string{"HOST": "db.internal", "EMPTY": ""}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	got, err := Interpolate("f", "postgres://${HOST}:${PORT:-5432}/app", lookup)
	if err != nil || got != "postgres://db.internal:5432/app" {
		t.Fatalf("got %q, %v", got, err)
	}
	var mv *MissingVarError
	if _, err := Interpolate("environments.production.database_url", "${SECRET}/${EMPTY}", lookup); !errors.As(err, &mv) || len(mv.Vars) != 2 {
		t.Fatalf("expected both missing vars reported, got %v", err)
	}
	if refs := References("${A} and ${B:-x}"); len(refs) != 2 || refs[1] != "B" {
		t.Errorf("refs = %v", refs)
	}
}

func TestDatabaseName(t *testing.T) {
	cases := map[string]string{"my-app": "my_app", "2fast": "db_2fast", "Shop.API": "shop_api"}
	for in, want := range cases {
		if got := DatabaseName(in); got != want {
			t.Errorf("DatabaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

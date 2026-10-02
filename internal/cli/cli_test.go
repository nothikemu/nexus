package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/pgtest"
)

type result struct {
	out, err string
	code     int
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, &out, &errb)
	return result{out.String(), errb.String(), code}
}

func (r result) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.out), v); err != nil {
		t.Fatalf("invalid JSON (%v):\n%s\nstderr:\n%s", err, r.out, r.err)
	}
}

func TestHelpVersionMascot(t *testing.T) {
	r := run(t, "--help", "--plain")
	for _, want := range []string{"nexus", "START", "DATABASE", "init", "migration", "GLOBAL FLAGS", "--json"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("help missing %q:\n%s", want, r.out)
		}
	}
	var v map[string]string
	run(t, "version", "--json").json(t, &v)
	if v["version"] == "" {
		t.Errorf("version = %v", v)
	}
	var states []map[string]string
	run(t, "mascot", "--json").json(t, &states)
	if len(states) != 10 {
		t.Errorf("mascot states = %d", len(states))
	}
	r = run(t, "statsu", "--plain")
	if r.code != 2 || !strings.Contains(r.err, "did you mean nexus status?") {
		t.Errorf("unknown command: code=%d stderr=%s", r.code, r.err)
	}
}

func TestOutsideProject(t *testing.T) {
	r := run(t, "-C", t.TempDir(), "migration", "status", "--json")
	if r.code != 2 || !strings.Contains(r.err, `"code": "no_project"`) {
		t.Errorf("code=%d stderr=%s", r.code, r.err)
	}
}

func TestInitScaffolds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-app")
	var res struct {
		Project string   `json:"project"`
		Created []string `json:"created"`
		Port    int      `json:"port"`
	}
	r := run(t, "init", dir, "--json")
	if r.code != 0 {
		t.Fatalf("init failed: %s", r.err)
	}
	r.json(t, &res)
	if res.Project != "demo-app" || len(res.Created) != 4 || res.Port == 0 {
		t.Errorf("init = %+v", res)
	}
	if r := run(t, "init", dir, "--json"); r.code != 2 {
		t.Errorf("re-init code = %d", r.code)
	}
}

// testProject scaffolds a project pointed at a fresh test database.
func testProject(t *testing.T, extra string) string {
	t.Helper()
	url := pgtest.URL(t)
	dir := filepath.Join(t.TempDir(), "app")
	if r := run(t, "init", dir, "--json"); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	t.Setenv("NEXUS_CLI_TEST_URL", url)
	cfg := "project:\n  name: app\ndatabase:\n  url: ${NEXUS_CLI_TEST_URL}\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "nexus.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProjectWorkflow(t *testing.T) {
	dir := testProject(t, "")
	c := func(args ...string) result { return run(t, append([]string{"-C", dir, "--json"}, args...)...) }

	var st struct{ Pending, Applied int }
	c("migration", "status").json(t, &st)
	if st.Pending != 1 || st.Applied != 0 {
		t.Fatalf("status = %+v", st)
	}

	var preview struct {
		DryRun bool `json:"dry_run"`
		Diff   struct {
			Changes []struct{ Kind, Name string } `json:"changes"`
		} `json:"diff"`
	}
	c("migration", "diff").json(t, &preview)
	if !preview.DryRun || len(preview.Diff.Changes) == 0 || preview.Diff.Changes[0].Name != "public.users" {
		t.Fatalf("preview = %+v", preview)
	}

	var applied struct{ Applied []any }
	c("migration", "apply").json(t, &applied)
	if len(applied.Applied) != 1 {
		t.Fatalf("apply = %+v", applied)
	}
	if r := c("db", "seed"); r.code != 0 {
		t.Fatalf("seed: %s", r.err)
	}

	var q []struct {
		Rows []map[string]any `json:"rows"`
	}
	c("sql", "select count(*)::int as n from users").json(t, &q)
	if len(q) != 1 || q[0].Rows[0]["n"] != float64(3) {
		t.Fatalf("sql = %+v", q)
	}

	var table struct{ Columns []any }
	c("table", "users").json(t, &table)
	if len(table.Columns) != 4 {
		t.Errorf("columns = %d", len(table.Columns))
	}
	var rowsOut struct{ Total int }
	c("table", "users", "rows", "--search", "lovelace").json(t, &rowsOut)
	if rowsOut.Total != 1 {
		t.Errorf("search total = %d", rowsOut.Total)
	}

	var status struct {
		Online bool `json:"online"`
		Tables int  `json:"tables"`
	}
	c("status").json(t, &status)
	if !status.Online || status.Tables != 1 {
		t.Errorf("status = %+v", status)
	}

	var plan struct {
		Plan     map[string]any `json:"plan"`
		Insights []any          `json:"insights"`
	}
	c("query", "analyze", "select * from users where name = 'Ada Lovelace'").json(t, &plan)
	if plan.Plan == nil {
		t.Error("no plan")
	}

	// SQL errors exit 1 and say what PostgreSQL said.
	if r := c("sql", "select * from nope"); r.code != 1 || !strings.Contains(r.err, `relation \"nope\" does not exist`) {
		t.Errorf("sql error: code=%d stderr=%s", r.code, r.err)
	}

	// Rolling back is destructive: without a TTY it needs --yes.
	if r := c("migration", "rollback"); r.code != 3 || !strings.Contains(r.err, "confirmation_required") {
		t.Errorf("rollback without --yes: code=%d stderr=%s", r.code, r.err)
	}
	var rb struct{ Reverted []any }
	c("migration", "rollback", "--yes").json(t, &rb)
	if len(rb.Reverted) != 1 {
		t.Errorf("rollback = %+v", rb)
	}
}

func TestProtectedEnvironment(t *testing.T) {
	dir := testProject(t, "environments:\n  production:\n    database_url: ${NEXUS_CLI_TEST_URL}\n")
	c := func(args ...string) result {
		return run(t, append([]string{"-C", dir, "--json", "--env", "production"}, args...)...)
	}
	// --yes alone never confirms a destructive operation on production.
	if r := c("sql", "drop table if exists anything", "--yes"); r.code != 3 || !strings.Contains(r.err, "DELETE PRODUCTION DATA") {
		t.Errorf("drop on production: code=%d stderr=%s", r.code, r.err)
	}
	if r := c("sql", "drop table if exists anything", "--confirm", "DELETE PRODUCTION DATA"); r.code != 0 {
		t.Errorf("confirmed drop: code=%d stderr=%s", r.code, r.err)
	}
	// Reads need no confirmation.
	if r := c("sql", "select 1"); r.code != 0 {
		t.Errorf("read on production: code=%d stderr=%s", r.code, r.err)
	}
	if r := run(t, "-C", dir, "--json", "--env", "staging", "status"); r.code != 2 || !strings.Contains(r.err, "unknown_environment") {
		t.Errorf("unknown env: code=%d stderr=%s", r.code, r.err)
	}
}

func TestMissingSecretFailsLoudly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if r := run(t, "init", dir, "--json"); r.code != 0 {
		t.Fatal(r.err)
	}
	cfg := "project:\n  name: app\nenvironments:\n  production:\n    database_url: ${NEXUS_DEFINITELY_UNSET_VAR}\n"
	if err := os.WriteFile(filepath.Join(dir, "nexus.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	r := run(t, "-C", dir, "--json", "--env", "production", "status")
	if r.code != 2 || !strings.Contains(r.err, "NEXUS_DEFINITELY_UNSET_VAR") {
		t.Errorf("code=%d stderr=%s", r.code, r.err)
	}
}

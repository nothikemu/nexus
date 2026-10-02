package localdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/project"
)

func TestPickAndOrdering(t *testing.T) {
	all := []Binaries{{Version: "15.8", Major: 15}, {Version: "17.2", Major: 17}, {Version: "16.4", Major: 16}}
	if !versionGreater("17.2", "16.4") || versionGreater("16.4", "16.10") {
		t.Error("versionGreater is wrong")
	}
	if b, exact, ok := pick(all, 16); !ok || !exact || b.Version != "16.4" {
		t.Errorf("pick 16 = %+v %v %v", b, exact, ok)
	}
	if b, exact, ok := pick(all, 14); !ok || exact || b.Version != "15.8" {
		t.Errorf("pick 14 = %+v %v %v (falls back to the first, newest-sorted entry)", b, exact, ok)
	}
	if _, _, ok := pick(nil, 16); ok {
		t.Error("pick on nothing should fail")
	}
}

func testProject(t *testing.T) *project.Project {
	t.Helper()
	dir := t.TempDir()
	res, err := project.Scaffold(project.ScaffoldOptions{Dir: dir, Name: "demo-app", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDockerArgsBindLocalhostOnly(t *testing.T) {
	p := testProject(t)
	d := newDocker(p)
	args := strings.Join(d.runArgs("s3cret"), " ")
	for _, want := range []string{
		"--name nexus-demo-app-db",
		"-p 127.0.0.1:" + itoa(p.Config.Database.Port) + ":5432",
		"-v nexus-demo-app-pgdata:/var/lib/postgresql/data",
		"POSTGRES_DB=demo_app",
		"postgres:16",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("docker args missing %q:\n%s", want, args)
		}
	}
	p.Config.Database.Version = 18
	if args := strings.Join(d.runArgs("x"), " "); !strings.Contains(args, ":/var/lib/postgresql ") {
		t.Errorf("pg18 mount wrong: %s", args)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestTailFileAndPostmasterPID(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	_ = os.WriteFile(log, []byte("a\nb\nc\nd\n"), 0o600)
	if got := tailFile(log, 2); got != "c\nd\n" {
		t.Errorf("tailFile = %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "postmaster.pid"), []byte("4242\n/data\n1700000000\n54320\n\n"), 0o600)
	pid, port, ok := readPostmasterPID(dir)
	if !ok || pid != 4242 || port != 54320 {
		t.Errorf("pid=%d port=%d ok=%v", pid, port, ok)
	}
}

func TestLocalURL(t *testing.T) {
	p := testProject(t)
	u := localURL(p, "p@ss/word")
	tgt := pg.Describe(u)
	if tgt.User != "postgres" || tgt.Host != "127.0.0.1" || tgt.Database != "demo_app" {
		t.Errorf("target = %+v from %s", tgt, u)
	}
}

// TestNativeLifecycle runs a real cluster. It needs PostgreSQL binaries and
// a non-root user, so it is skipped elsewhere.
func TestNativeLifecycle(t *testing.T) {
	if isRoot() {
		t.Skip("PostgreSQL can't run as root")
	}
	ctx := context.Background()
	p := testProject(t)
	p.Config.Database.Runtime = config.RuntimeNative
	n := newNative(ctx, p)
	if err := n.Check(ctx); errors.Is(err, ErrNoBinaries) {
		t.Skip("no PostgreSQL binaries installed")
	}
	t.Cleanup(func() { _ = n.Destroy(context.Background()) })

	res, err := n.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.AlreadyRunning {
		t.Errorf("first start = %+v", res)
	}
	st, err := n.Status(ctx)
	if err != nil || !st.Running || st.PID == 0 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	url, _ := n.URL()
	pool, err := pg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := pg.ServerVersion(ctx, pool)
	pool.Close()
	if err != nil || v == "" {
		t.Fatalf("version = %q, %v", v, err)
	}
	again, err := n.Start(ctx)
	if err != nil || !again.AlreadyRunning || again.Created {
		t.Errorf("second start = %+v, %v", again, err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); !errors.Is(err, ErrNotRunning) {
		t.Errorf("second stop = %v", err)
	}
	// Restart keeps data and doesn't re-create.
	res, err = n.Start(ctx)
	if err != nil || res.Created {
		t.Fatalf("restart = %+v, %v", res, err)
	}
	var b strings.Builder
	if err := n.Logs(ctx, &b, 50, false); err != nil || !strings.Contains(b.String(), "ready to accept connections") {
		t.Errorf("logs = %q, %v", b.String(), err)
	}
}

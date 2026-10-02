package localdb

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/project"
)

// Native manages a PostgreSQL cluster in .nexus/postgres using the
// PostgreSQL binaries installed on this machine.
type Native struct {
	proj *project.Project
	all  []Binaries
}

func newNative(ctx context.Context, p *project.Project) *Native {
	return &Native{proj: p, all: FindBinaries(ctx)}
}

// Kind implements Runtime.
func (n *Native) Kind() config.Runtime { return config.RuntimeNative }

func nativeDataDir(p *project.Project) string { return filepath.Join(p.StateDir(), "postgres", "data") }

func (n *Native) dataDir() string { return nativeDataDir(n.proj) }
func (n *Native) logFile() string {
	return filepath.Join(n.proj.StateDir(), "postgres", "postgres.log")
}

// clusterMajor is the major version of an existing cluster (0 if none).
func (n *Native) clusterMajor() int {
	data, err := os.ReadFile(filepath.Join(n.dataDir(), "PG_VERSION"))
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return v
}

// binaries picks the installation to use: the cluster's own major when one
// exists (data directories are not portable across majors), otherwise the
// configured major, falling back to the newest installed.
func (n *Native) binaries() (Binaries, []string, error) {
	if len(n.all) == 0 {
		return Binaries{}, nil, ErrNoBinaries
	}
	if major := n.clusterMajor(); major != 0 {
		b, exact, _ := pick(n.all, major)
		if !exact {
			return Binaries{}, nil, fmt.Errorf("this project's cluster was created with PostgreSQL %d, which is no longer installed (found %s)", major, versions(n.all))
		}
		return b, nil, nil
	}
	want := n.proj.Config.Database.Version
	b, exact, _ := pick(n.all, want)
	var warnings []string
	if !exact {
		warnings = append(warnings, fmt.Sprintf("PostgreSQL %d isn't installed; using %s instead", want, b.Version))
	}
	return b, warnings, nil
}

func versions(all []Binaries) string {
	vs := make([]string, len(all))
	for i, b := range all {
		vs[i] = b.Version
	}
	return strings.Join(vs, ", ")
}

// Check implements Runtime.
func (n *Native) Check(context.Context) error {
	if isRoot() {
		return ErrRoot
	}
	_, _, err := n.binaries()
	return err
}

// URL implements Runtime.
func (n *Native) URL() (string, error) {
	st, err := n.proj.LocalState()
	if err != nil {
		return "", err
	}
	return localURL(n.proj, st.Password), nil
}

// Status implements Runtime.
func (n *Native) Status(ctx context.Context) (*Status, error) {
	st := &Status{Runtime: config.RuntimeNative, Port: n.proj.Config.Database.Port, Location: n.proj.Rel(n.dataDir())}
	major := n.clusterMajor()
	if major == 0 {
		return st, nil
	}
	st.Initialized = true
	st.Version = strconv.Itoa(major)
	if b, _, err := n.binaries(); err == nil {
		st.Version = b.Version
	}
	pid, port, ok := readPostmasterPID(n.dataDir())
	if !ok || !processAlive(pid) {
		return st, nil
	}
	st.Running, st.PID = true, pid
	if port != 0 {
		st.Port = port
	}
	return st, nil
}

// Start implements Runtime.
func (n *Native) Start(ctx context.Context) (*StartResult, error) {
	if isRoot() {
		return nil, ErrRoot
	}
	b, warnings, err := n.binaries()
	if err != nil {
		return nil, err
	}
	res := &StartResult{Version: b.Version, Warnings: warnings}
	cfg := n.proj.Config.Database

	st, err := n.Status(ctx)
	if err != nil {
		return nil, err
	}
	if st.Running {
		res.AlreadyRunning = true
		if st.Port != cfg.Port {
			res.Warnings = append(res.Warnings, fmt.Sprintf("running on port %d, but %s says %d — restart with `nexus dev stop` then `nexus dev`", st.Port, config.FileName, cfg.Port))
		}
		created, err := n.ensureDatabase(ctx)
		res.Created = created
		return res, err
	}

	local, err := n.proj.LocalState()
	if err != nil {
		return nil, err
	}
	if !st.Initialized {
		if err := n.initdb(ctx, b, local.Password); err != nil {
			return nil, err
		}
	}
	if err := n.writeConfig(b); err != nil {
		return nil, err
	}
	if !project.PortFree(cfg.Port) {
		return nil, &PortInUseError{Port: cfg.Port}
	}
	if err := os.MkdirAll(filepath.Dir(n.logFile()), 0o700); err != nil {
		return nil, err
	}
	// pg_ctl -w waits until the server accepts connections.
	if _, err := run(ctx, b.Path("pg_ctl"), "start", "-D", n.dataDir(), "-l", n.logFile(), "-w", "-t", "60", "-s"); err != nil {
		return nil, fmt.Errorf("%w\n\nlast log lines:\n%s", err, tailFile(n.logFile(), 8))
	}
	url, _ := n.URL()
	if err := waitReady(ctx, url, 30*time.Second); err != nil {
		return nil, err
	}
	created, err := n.ensureDatabase(ctx)
	res.Created = created || !st.Initialized
	return res, err
}

func (n *Native) initdb(ctx context.Context, b Binaries, password string) error {
	if err := os.MkdirAll(filepath.Dir(n.dataDir()), 0o700); err != nil {
		return err
	}
	pw, err := os.CreateTemp(n.proj.StateDir(), "pwfile-*")
	if err != nil {
		return err
	}
	defer os.Remove(pw.Name())
	if _, err := pw.WriteString(password + "\n"); err != nil {
		pw.Close()
		return err
	}
	if err := pw.Close(); err != nil {
		return err
	}
	_, err = run(ctx, b.Path("initdb"),
		"-D", n.dataDir(),
		"-U", Superuser,
		"--pwfile="+pw.Name(),
		"--auth=scram-sha-256",
		"--encoding=UTF8",
		"--no-locale",
		"--no-instructions",
	)
	if err != nil {
		_ = os.RemoveAll(n.dataDir())
		return err
	}
	// Pull Nexus-managed settings in from a separate file.
	f, err := os.OpenFile(filepath.Join(n.dataDir(), "postgresql.conf"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n# Settings managed by nexus (regenerated by `nexus dev`).\ninclude_if_exists = 'nexus.conf'\n")
	return err
}

// writeConfig (re)generates nexus.conf from nexus.yaml.
func (n *Native) writeConfig(b Binaries) error {
	var c strings.Builder
	c.WriteString("# Managed by nexus. Regenerated on every `nexus dev`; put your own settings in postgresql.conf.\n")
	fmt.Fprintf(&c, "port = %d\n", n.proj.Config.Database.Port)
	c.WriteString("listen_addresses = '127.0.0.1'\n")
	c.WriteString("unix_socket_directories = ''\n")
	c.WriteString("log_line_prefix = '%m [%p] '\n")
	c.WriteString("log_statement = 'ddl'\n")
	c.WriteString("log_min_duration_statement = 500\n")
	c.WriteString("track_io_timing = on\n")
	if hasExtension(b, "pg_stat_statements") {
		c.WriteString("shared_preload_libraries = 'pg_stat_statements'\n")
	}
	return os.WriteFile(filepath.Join(n.dataDir(), "nexus.conf"), []byte(c.String()), 0o600)
}

// hasExtension reports whether an extension's library and control file are
// installed alongside the binaries (preloading a missing library would stop
// the server from starting).
func hasExtension(b Binaries, name string) bool {
	pgc := b.Path("pg_config")
	if _, err := os.Stat(pgc); err != nil {
		return false
	}
	out, err := exec.Command(pgc, "--pkglibdir", "--sharedir").Output()
	if err != nil {
		return false
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 {
		return false
	}
	libs, _ := filepath.Glob(filepath.Join(lines[0], name+".*"))
	_, ctlErr := os.Stat(filepath.Join(lines[1], "extension", name+".control"))
	return len(libs) > 0 && ctlErr == nil
}

// ensureDatabase creates the project database when missing.
func (n *Native) ensureDatabase(ctx context.Context) (bool, error) {
	local, err := n.proj.LocalState()
	if err != nil {
		return false, err
	}
	return createDatabaseIfMissing(ctx, n.proj, local.Password)
}

// createDatabaseIfMissing connects to the maintenance database and creates
// the project database. Shared with the Docker runtime.
func createDatabaseIfMissing(ctx context.Context, p *project.Project, password string) (bool, error) {
	admin := localURL(p, password)
	admin = strings.Replace(admin, "/"+p.Config.Database.Name+"?", "/postgres?", 1)
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(c, admin)
	if err != nil {
		return false, err
	}
	defer conn.Close(context.Background())
	var exists bool
	if err := conn.QueryRow(c, `select exists (select 1 from pg_database where datname = $1)`, p.Config.Database.Name).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	_, err = conn.Exec(c, "create database "+pgx.Identifier{p.Config.Database.Name}.Sanitize())
	return err == nil, err
}

// Stop implements Runtime.
func (n *Native) Stop(ctx context.Context) error {
	st, err := n.Status(ctx)
	if err != nil {
		return err
	}
	if !st.Running {
		return ErrNotRunning
	}
	b, _, err := n.binaries()
	if err != nil {
		return err
	}
	_, err = run(ctx, b.Path("pg_ctl"), "stop", "-D", n.dataDir(), "-m", "fast", "-w", "-t", "60", "-s")
	return err
}

// Destroy implements Runtime.
func (n *Native) Destroy(ctx context.Context) error {
	if err := n.Stop(ctx); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return os.RemoveAll(filepath.Join(n.proj.StateDir(), "postgres"))
}

// Logs implements Runtime.
func (n *Native) Logs(ctx context.Context, w io.Writer, lines int, follow bool) error {
	return tailAndFollow(ctx, n.logFile(), w, lines, follow)
}

// readPostmasterPID reads the PID and port from postmaster.pid.
func readPostmasterPID(dataDir string) (pid, port int, ok bool) {
	f, err := os.Open(filepath.Join(dataDir, "postmaster.pid"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() && len(lines) < 4 {
		lines = append(lines, strings.TrimSpace(sc.Text()))
	}
	if len(lines) < 1 {
		return 0, 0, false
	}
	pid, err = strconv.Atoi(lines[0])
	if err != nil {
		return 0, 0, false
	}
	if len(lines) >= 4 {
		port, _ = strconv.Atoi(lines[3])
	}
	return pid, port, true
}

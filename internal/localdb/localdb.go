// Package localdb runs the local development database. It manages a
// PostgreSQL cluster natively (initdb/pg_ctl inside .nexus/), in a Docker
// container, or connects to an external server it never touches.
package localdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/project"
)

// Superuser is the role local clusters are created with.
const Superuser = "postgres"

// Status describes the local database.
type Status struct {
	Runtime     config.Runtime `json:"runtime"`
	Initialized bool           `json:"initialized"`
	Running     bool           `json:"running"`
	Version     string         `json:"version,omitempty"`
	PID         int            `json:"pid,omitempty"`
	Port        int            `json:"port,omitempty"`
	Location    string         `json:"location,omitempty"` // data dir, container or host
}

// StartResult reports what Start did.
type StartResult struct {
	AlreadyRunning bool     `json:"already_running"`
	Created        bool     `json:"created"` // a fresh, empty database was created
	Version        string   `json:"version"`
	Warnings       []string `json:"warnings,omitempty"`
}

// Runtime manages one project's local database.
type Runtime interface {
	Kind() config.Runtime
	// Check verifies prerequisites (binaries, Docker daemon) without side effects.
	Check(ctx context.Context) error
	Status(ctx context.Context) (*Status, error)
	Start(ctx context.Context) (*StartResult, error)
	Stop(ctx context.Context) error
	// Destroy stops the database and deletes all of its data.
	Destroy(ctx context.Context) error
	// Logs writes the last `lines` lines of server logs, then follows if asked.
	Logs(ctx context.Context, w io.Writer, lines int, follow bool) error
	// URL is the connection string, including credentials.
	URL() (string, error)
}

// Errors with stable identities for the CLI to explain.
var (
	ErrNotRunning = errors.New("the local database isn't running")
	ErrExternal   = errors.New("nexus doesn't start or stop external databases")
	ErrRoot       = errors.New("PostgreSQL refuses to run as root")
	ErrNoBinaries = errors.New("no PostgreSQL installation found")
	ErrNoDocker   = errors.New("docker isn't available")
)

// PortInUseError means another process owns the configured port.
type PortInUseError struct{ Port int }

func (e *PortInUseError) Error() string {
	return fmt.Sprintf("port %d is already in use by another process", e.Port)
}

// New returns the runtime for a project, resolving `auto`.
func New(ctx context.Context, p *project.Project) (Runtime, error) {
	kind, _ := Resolve(ctx, p)
	switch kind {
	case config.RuntimeExternal:
		return &External{proj: p}, nil
	case config.RuntimeDocker:
		return newDocker(p), nil
	default:
		return newNative(ctx, p), nil
	}
}

// Resolve decides which runtime a project uses and why.
func Resolve(ctx context.Context, p *project.Project) (config.Runtime, string) {
	rt := p.Config.Database.Runtime
	if rt != config.RuntimeAuto {
		return rt, "set in " + config.FileName
	}
	// Stick with whatever already holds this project's data.
	if _, err := os.Stat(filepath.Join(nativeDataDir(p), "PG_VERSION")); err == nil {
		return config.RuntimeNative, "existing local cluster in " + project.StateDirName + "/"
	}
	d := newDocker(p)
	if !isRoot() && len(FindBinaries(ctx)) > 0 {
		return config.RuntimeNative, "PostgreSQL is installed on this machine"
	}
	if d.Check(ctx) == nil {
		if isRoot() {
			return config.RuntimeDocker, "running as root, where PostgreSQL can't run natively"
		}
		return config.RuntimeDocker, "PostgreSQL isn't installed, but Docker is available"
	}
	return config.RuntimeNative, "default"
}

func isRoot() bool {
	return runtime.GOOS != "windows" && os.Geteuid() == 0
}

// localURL builds the connection string for a locally managed database.
func localURL(p *project.Project, password string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(Superuser, password),
		Host:     "127.0.0.1:" + strconv.Itoa(p.Config.Database.Port),
		Path:     "/" + p.Config.Database.Name,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// waitReady polls until the server answers. A missing project database
// still counts as ready: the server is up and the caller creates it.
func waitReady(ctx context.Context, connString string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		pool, err := pg.Open(c, connString)
		cancel()
		if err == nil {
			pool.Close()
			return nil
		}
		if !pg.IsUnreachable(err) {
			if pg.IsMissingDatabase(err) {
				return nil
			}
			return err
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("database didn't become ready within %s: %w", timeout, last)
}

// run executes a command, folding its output into the error on failure.
func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return string(out), fmt.Errorf("%s %s: %w\n%s", filepath.Base(name), firstArg(args), err, msg)
	}
	return string(out), nil
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

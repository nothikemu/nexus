package localdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/project"
)

// Docker manages a PostgreSQL container with a named volume per project.
type Docker struct {
	proj *project.Project
	bin  string
}

func newDocker(p *project.Project) *Docker {
	bin, _ := exec.LookPath("docker")
	return &Docker{proj: p, bin: bin}
}

// Kind implements Runtime.
func (d *Docker) Kind() config.Runtime { return config.RuntimeDocker }

// Container is the container name for the project.
func (d *Docker) Container() string { return "nexus-" + d.proj.Name() + "-db" }

// Volume is the named volume holding the project's data.
func (d *Docker) Volume() string { return "nexus-" + d.proj.Name() + "-pgdata" }

// Image is the PostgreSQL image to run.
func (d *Docker) Image() string {
	if img := d.proj.Config.Database.Image; img != "" {
		return img
	}
	return fmt.Sprintf("postgres:%d", d.proj.Config.Database.Version)
}

// Check implements Runtime.
func (d *Docker) Check(ctx context.Context) error {
	if d.bin == "" {
		return ErrNoDocker
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := run(c, d.bin, "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("%w: the Docker daemon isn't reachable", ErrNoDocker)
	}
	return nil
}

// URL implements Runtime.
func (d *Docker) URL() (string, error) {
	st, err := d.proj.LocalState()
	if err != nil {
		return "", err
	}
	return localURL(d.proj, st.Password), nil
}

// inspect returns the container state ("" when it doesn't exist).
func (d *Docker) inspect(ctx context.Context) (state string, pid int, err error) {
	out, err := exec.CommandContext(ctx, d.bin, "inspect", "-f", "{{.State.Status}}|{{.State.Pid}}", d.Container()).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", 0, nil // no such container
		}
		return "", 0, err
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) == 2 {
		pid, _ = strconv.Atoi(parts[1])
	}
	return parts[0], pid, nil
}

// Status implements Runtime.
func (d *Docker) Status(ctx context.Context) (*Status, error) {
	st := &Status{Runtime: config.RuntimeDocker, Port: d.proj.Config.Database.Port, Location: d.Container(), Version: d.Image()}
	if err := d.Check(ctx); err != nil {
		return st, err
	}
	state, pid, err := d.inspect(ctx)
	if err != nil {
		return st, err
	}
	st.Initialized = state != ""
	st.Running = state == "running"
	if st.Running {
		st.PID = pid
	}
	return st, nil
}

// runArgs builds the `docker run` arguments. The port binds to 127.0.0.1
// only, so the database is never exposed on the network.
func (d *Docker) runArgs(password string) []string {
	cfg := d.proj.Config.Database
	mount := "/var/lib/postgresql/data"
	if cfg.Version >= 18 {
		mount = "/var/lib/postgresql" // PostgreSQL 18+ images keep versioned data dirs here
	}
	return []string{
		"run", "-d",
		"--name", d.Container(),
		"--label", "dev.nexus.project=" + d.proj.Name(),
		"-e", "POSTGRES_USER=" + Superuser,
		"-e", "POSTGRES_PASSWORD=" + password,
		"-e", "POSTGRES_DB=" + cfg.Name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", cfg.Port),
		"-v", d.Volume() + ":" + mount,
		d.Image(),
		"-c", "shared_preload_libraries=pg_stat_statements",
		"-c", "log_statement=ddl",
		"-c", "log_min_duration_statement=500",
		"-c", "track_io_timing=on",
	}
}

// Start implements Runtime.
func (d *Docker) Start(ctx context.Context) (*StartResult, error) {
	if err := d.Check(ctx); err != nil {
		return nil, err
	}
	res := &StartResult{Version: d.Image()}
	local, err := d.proj.LocalState()
	if err != nil {
		return nil, err
	}
	state, _, err := d.inspect(ctx)
	if err != nil {
		return nil, err
	}
	switch state {
	case "running":
		res.AlreadyRunning = true
	case "":
		if !project.PortFree(d.proj.Config.Database.Port) {
			return nil, &PortInUseError{Port: d.proj.Config.Database.Port}
		}
		if _, err := run(ctx, d.bin, d.runArgs(local.Password)...); err != nil {
			return nil, err
		}
		res.Created = true
	default:
		if _, err := run(ctx, d.bin, "start", d.Container()); err != nil {
			return nil, err
		}
	}
	url := localURL(d.proj, local.Password)
	// First boot initialises the cluster; pulling the image may already have taken a while.
	if err := waitReady(ctx, url, 90*time.Second); err != nil {
		return nil, err
	}
	created, err := createDatabaseIfMissing(ctx, d.proj, local.Password)
	res.Created = res.Created || created
	return res, err
}

// Stop implements Runtime.
func (d *Docker) Stop(ctx context.Context) error {
	st, err := d.Status(ctx)
	if err != nil {
		return err
	}
	if !st.Running {
		return ErrNotRunning
	}
	_, err = run(ctx, d.bin, "stop", d.Container())
	return err
}

// Destroy implements Runtime.
func (d *Docker) Destroy(ctx context.Context) error {
	if err := d.Check(ctx); err != nil {
		return err
	}
	if state, _, _ := d.inspect(ctx); state != "" {
		if _, err := run(ctx, d.bin, "rm", "-f", "-v", d.Container()); err != nil {
			return err
		}
	}
	out, err := exec.CommandContext(ctx, d.bin, "volume", "ls", "-q", "--filter", "name=^"+d.Volume()+"$").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		if _, err := run(ctx, d.bin, "volume", "rm", d.Volume()); err != nil {
			return err
		}
	}
	return nil
}

// Logs implements Runtime.
func (d *Docker) Logs(ctx context.Context, w io.Writer, lines int, follow bool) error {
	args := []string{"logs", "--tail", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	cmd := exec.CommandContext(ctx, d.bin, append(args, d.Container())...)
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

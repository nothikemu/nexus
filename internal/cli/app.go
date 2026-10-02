// Package cli implements the nexus command tree.
package cli

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/project"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/version"
)

// Flags are the global flags shared by every command.
type Flags struct {
	JSON        bool
	Plain       bool
	NoColor     bool
	NoAnimation bool
	Env         string
	DBURL       string
	ProjectDir  string
	Yes         bool
	Confirm     string
	Verbose     bool
}

// App is the per-invocation context. Expensive resources — the project, the
// database pool — are loaded lazily, so commands that don't need them never
// pay for them.
type App struct {
	P     *ui.Printer
	Flags *Flags

	proj    *project.Project
	projErr error
	loaded  bool

	pool   *pgxpool.Pool
	target *Target
}

// Target is the resolved database an invocation talks to.
type Target struct {
	Env       string
	Protected bool
	URL       string
	Runtime   config.Runtime // for local
	Local     localdb.Runtime
}

// Safety returns the safety target description.
func (t *Target) Safety() safety.Target {
	return safety.Target{Env: t.Env, Protected: t.Protected, Database: pg.Describe(t.URL).String()}
}

// T is shorthand for the theme.
func (a *App) T() *ui.Theme { return a.P.T }

// Project loads the project from --project or the working directory.
func (a *App) Project() (*project.Project, error) {
	if !a.loaded {
		a.loaded = true
		dir := a.Flags.ProjectDir
		if dir == "" {
			dir = "."
		}
		a.proj, a.projErr = project.Find(dir)
	}
	return a.proj, a.projErr
}

// HasProject reports whether a project is available, without failing.
func (a *App) HasProject() bool {
	p, err := a.Project()
	return err == nil && p != nil
}

// EnvName is the selected environment.
func (a *App) EnvName() string {
	if a.Flags.Env != "" {
		return a.Flags.Env
	}
	if e := os.Getenv("NEXUS_ENV"); e != "" {
		return e
	}
	return "local"
}

// Target resolves which database this invocation uses.
func (a *App) Target(ctx context.Context) (*Target, error) {
	if a.target != nil {
		return a.target, nil
	}
	url := a.Flags.DBURL
	if url == "" {
		url = os.Getenv("NEXUS_DATABASE_URL")
	}
	if url != "" {
		a.target = &Target{Env: "url", URL: url}
		return a.target, nil
	}
	p, err := a.Project()
	if err != nil {
		return nil, err
	}
	env := a.EnvName()
	if env == "local" {
		rt, err := localdb.New(ctx, p)
		if err != nil {
			return nil, err
		}
		u, err := rt.URL()
		if err != nil {
			return nil, err
		}
		a.target = &Target{Env: "local", URL: u, Runtime: rt.Kind(), Local: rt}
		return a.target, nil
	}
	e, err := p.Config.Environment(env)
	if err != nil {
		return nil, unknownEnvProblem(env, p.Config.EnvironmentNames())
	}
	u, err := config.Interpolate("environments."+env+".database_url", e.DatabaseURL, nil)
	if err != nil {
		return nil, err
	}
	a.target = &Target{Env: env, Protected: e.IsProtected(env), URL: u}
	return a.target, nil
}

// DB opens (once) and returns the connection pool for the target.
func (a *App) DB(ctx context.Context) (*pgxpool.Pool, error) {
	if a.pool != nil {
		return a.pool, nil
	}
	t, err := a.Target(ctx)
	if err != nil {
		return nil, err
	}
	pool, err := pg.Open(ctx, t.URL)
	if err != nil {
		return nil, a.connectProblem(ctx, t, err)
	}
	a.pool = pool
	return pool, nil
}

// connectProblem explains a failed connection in terms of what to do next.
func (a *App) connectProblem(ctx context.Context, t *Target, err error) error {
	where := pg.Describe(t.URL)
	switch {
	case pg.IsUnreachable(err) && t.Env == "local":
		pr := &ui.Problem{Title: ui.SayFirst(ui.MomentAsleep), Detail: "nothing is answering on " + where.Host + ":" + itoa(int(where.Port)) + ".", Code: "database_unreachable", Exit: ui.ExitUnreachable, Err: err}
		if t.Local != nil {
			if st, serr := t.Local.Status(ctx); serr == nil && st.Initialized && !st.Running {
				pr.Detail = "the local database is stopped."
			}
		}
		return pr.WithHint("wake it with %s", a.T().Cmd("nexus dev"))
	case pg.IsUnreachable(err):
		return &ui.Problem{Title: "couldn't reach the " + t.Env + " database.", Detail: err.Error(), Code: "database_unreachable", Exit: ui.ExitUnreachable, Err: err}
	case pg.IsAuthError(err):
		pr := &ui.Problem{Title: "the database rejected the credentials.", Detail: err.Error(), Code: "auth_failed", Exit: ui.ExitUnreachable, Err: err}
		if t.Env == "local" {
			pr.Hint = "the password in .nexus/local.json no longer matches this database. If you recreated it by hand, run " + a.T().Cmd("nexus dev reset")
		}
		return pr
	case pg.IsMissingDatabase(err):
		pr := &ui.Problem{Title: "database " + where.Database + " doesn't exist.", Detail: err.Error(), Code: "database_missing", Exit: ui.ExitUnreachable, Err: err}
		if t.Env == "local" {
			pr.Hint = "create it with " + a.T().Cmd("nexus dev")
		}
		return pr
	}
	return err
}

// Schemas returns the schemas Nexus works with: the project's configured
// schemas, or every user schema when exploring a database via --db-url.
func (a *App) Schemas(ctx context.Context) ([]string, error) {
	if a.Flags.DBURL == "" && os.Getenv("NEXUS_DATABASE_URL") == "" {
		if p, err := a.Project(); err == nil {
			return p.Config.Database.Schemas, nil
		}
	}
	pool, err := a.DB(ctx)
	if err != nil {
		return nil, err
	}
	all, err := introspect.Schemas(ctx, pool)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range all {
		if s != migrate.Schema && !strings.HasPrefix(s, "pg_") {
			out = append(out, s)
		}
	}
	return out, nil
}

// Snapshot introspects the target database.
func (a *App) Snapshot(ctx context.Context) (*introspect.Snapshot, error) {
	pool, err := a.DB(ctx)
	if err != nil {
		return nil, err
	}
	schemas, err := a.Schemas(ctx)
	if err != nil {
		return nil, err
	}
	return introspect.Load(ctx, pool, schemas)
}

// Engine returns the migration engine for the project and target.
func (a *App) Engine(ctx context.Context) (*migrate.Engine, error) {
	p, err := a.Project()
	if err != nil {
		return nil, err
	}
	pool, err := a.DB(ctx)
	if err != nil {
		return nil, err
	}
	return &migrate.Engine{Pool: pool, Dir: p.MigrationsDir(), Schemas: p.Config.Database.Schemas, NexusVersion: version.Version}, nil
}

// Guard returns the confirmation guard.
func (a *App) Guard() *safety.Guard {
	return &safety.Guard{P: a.P, Yes: a.Flags.Yes, Confirm: a.Flags.Confirm}
}

// Allow checks an operation against the current target.
func (a *App) Allow(ctx context.Context, op safety.Operation) error {
	t, err := a.Target(ctx)
	if err != nil {
		return err
	}
	return a.Guard().Allow(op, t.Safety())
}

// Close releases resources.
func (a *App) Close() {
	if a.pool != nil {
		a.pool.Close()
	}
}

func unknownEnvProblem(env string, known []string) error {
	pr := &ui.Problem{Title: "there's no " + env + " environment.", Code: "unknown_environment", Exit: ui.ExitUsage}
	if len(known) == 0 {
		pr.Detail = "this project only has the local environment."
		pr.Hint = "add remote environments under `environments:` in " + config.FileName
	} else {
		pr.Detail = "known environments: local, " + strings.Join(known, ", ")
	}
	return pr
}

var errNotYet = errors.New("not built yet")

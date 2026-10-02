package cli

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/tui/browser"
	"github.com/nothikemu/nexus/internal/tui/dashboard"
	"os"
	"path/filepath"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/tui/repl"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

// runREPL opens the interactive SQL shell.
func runREPL(ctx context.Context, app *App) error {
	t, err := app.Target(ctx)
	if err != nil {
		return err
	}
	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	schemas, err := app.Schemas(ctx)
	if err != nil {
		return err
	}
	version, _, _ := pg.ServerVersion(ctx, pool)
	db := pg.Describe(t.URL).Database

	th := app.T()
	face := mascot.Face(th, mascot.Idle, 0)
	if face == "" {
		face = th.ToneGlyph(ui.ToneNexus)
	}
	envLabel := t.Env
	if t.Protected {
		envLabel = th.Warning.Render(t.Env + " · protected")
	}
	app.P.Block(face + "  " + th.Strong.Render("nexus sql") + th.Muted.Render(" · "+db+" · ") + envLabel + th.Muted.Render(" · PostgreSQL "+version) + "\n" +
		"         " + th.Faint.Render(`\? help · \q quit · end statements with ;`))

	return repl.Run(ctx, repl.Options{
		Theme:     th,
		Pool:      pool,
		Database:  db,
		Env:       t.Env,
		Protected: t.Protected,
		History:   historyPath(app),
		Schemas:   schemas,
	})
}

// historyPath keeps SQL history per project, or per user without a project.
func historyPath(app *App) string {
	if p, err := app.Project(); err == nil && app.Flags.DBURL == "" {
		return filepath.Join(p.StateDir(), "sql_history")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "nexus", "sql_history")
	}
	return ""
}

// runBrowser opens the interactive table explorer.
func runBrowser(ctx context.Context, app *App, snap *introspect.Snapshot, tb *introspect.Table) error {
	t, err := app.Target(ctx)
	if err != nil {
		return err
	}
	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	return browser.Run(ctx, browserOptions(app, t, pool, snap, tb))
}

func browserOptions(app *App, t *Target, pool *pgxpool.Pool, snap *introspect.Snapshot, tb *introspect.Table) browser.Options {
	o := browser.Options{Theme: app.T(), Pool: pool, Snapshot: snap, Table: tb}
	if t.Protected {
		o.ReadOnly = true
		o.ReadOnlyReason = "editing is off on " + t.Env + " — it's a protected environment."
	}
	return o
}

// runDashboard opens the live dashboard.
func runDashboard(ctx context.Context, app *App) error {
	t, err := app.Target(ctx)
	if err != nil {
		return err
	}
	src := dashboard.Source{Env: t.Env, Protected: t.Protected}
	if p, err := app.Project(); err == nil && t.Env != "url" {
		src.Project = p.Name()
		src.Schemas = p.Config.Database.Schemas
		dir := p.MigrationsDir()
		src.Migrations = func(ctx context.Context, pool *pgxpool.Pool) (*migrate.Status, error) {
			eng := &migrate.Engine{Pool: pool, Dir: dir}
			return eng.Status(ctx)
		}
	}
	if t.Local != nil {
		src.Runtime = string(t.Local.Kind())
		if t.Local.Kind() != config.RuntimeExternal {
			rt := t.Local
			src.Wake = func(ctx context.Context) error {
				_, err := rt.Start(ctx)
				return err
			}
		}
	}
	src.Connect = func(ctx context.Context) (*pgxpool.Pool, error) {
		pool, err := pg.Open(ctx, t.URL)
		if err != nil {
			return nil, app.connectProblem(ctx, t, err)
		}
		if src.Schemas == nil {
			all, err := app.Schemas(ctx)
			if err == nil {
				src.Schemas = all
			}
		}
		return pool, nil
	}
	if src.Schemas == nil {
		// Exploring an arbitrary database: discover schemas once connected.
		if pool, err := app.DB(ctx); err == nil {
			if s, err := app.Schemas(ctx); err == nil {
				src.Schemas = s
			}
			_ = pool
		}
	}
	return dashboard.Run(ctx, app.T(), src)
}

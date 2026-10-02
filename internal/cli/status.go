package cli

import (
	"context"
	"errors"
	"github.com/nothikemu/nexus/internal/render"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

// statusReport is everything `nexus status` knows, gathered once.
type statusReport struct {
	Project    string          `json:"project,omitempty"`
	Env        string          `json:"environment"`
	Online     bool            `json:"online"`
	Database   pg.Target       `json:"database"`
	URL        string          `json:"url"`
	Runtime    *localdb.Status `json:"runtime,omitempty"`
	Overview   *stats.Overview `json:"overview,omitempty"`
	Tables     int             `json:"tables"`
	Indexes    int             `json:"indexes"`
	Bytes      int64           `json:"schema_bytes"`
	Migrations *migrate.Status `json:"migrations,omitempty"`
	Problems   []stats.Finding `json:"problems"`
	Error      string          `json:"error,omitempty"`
}

func newStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "show what's running and how it's doing",
		Example: "  nexus status\n  nexus status --env staging\n  nexus status --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), app)
		},
	}
}

func runStatus(ctx context.Context, app *App) error {
	r, err := collectStatus(ctx, app)
	if err != nil {
		return err
	}
	if app.P.Mode().JSON {
		return app.P.JSON(r)
	}
	app.P.Block(renderStatusPanel(app.T(), r, true))
	return nil
}

// collectStatus gathers status without failing on an offline database:
// being asleep is a state, not an error.
func collectStatus(ctx context.Context, app *App) (*statusReport, error) {
	t, err := app.Target(ctx)
	if err != nil {
		return nil, err
	}
	r := &statusReport{Env: t.Env, Database: pg.Describe(t.URL), URL: pg.Redact(t.URL), Problems: []stats.Finding{}}
	if p, err := app.Project(); err == nil {
		r.Project = p.Name()
	}
	if t.Local != nil {
		if st, err := t.Local.Status(ctx); err == nil {
			r.Runtime = st
		}
	}
	pool, err := app.DB(ctx)
	if err != nil {
		var pr *ui.Problem
		if errors.As(err, &pr) && pr.Exit == ui.ExitUnreachable {
			r.Error = pr.Detail
			return r, nil
		}
		return nil, err
	}
	r.Online = true
	if r.Overview, err = stats.LoadOverview(ctx, pool); err != nil {
		return nil, err
	}
	schemas, err := app.Schemas(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := introspect.Load(ctx, pool, schemas)
	if err != nil {
		return nil, err
	}
	r.Tables = len(snap.BaseTables())
	r.Indexes = snap.IndexCount()
	r.Bytes = snap.TotalBytes()
	r.Problems = stats.Health(snap)
	if app.HasProject() && app.Flags.DBURL == "" {
		eng, err := app.Engine(ctx)
		if err == nil {
			if st, err := eng.Status(ctx); err == nil {
				r.Migrations = st
			}
		}
	}
	return r, nil
}

// renderStatusPanel draws the status panel. withNext adds next-step commands.
func renderStatusPanel(t *ui.Theme, r *statusReport, withNext bool) string {
	title := r.Env
	if r.Project != "" {
		title = r.Project + " " + t.Glyphs.Sep + " " + r.Env
	}
	badge := t.Badge(t.Glyphs.Dot+" online", t.Success)
	state := mascot.Idle
	headline := ui.SayFirst(ui.MomentConnected)
	if !r.Online {
		badge = t.Badge(t.Glyphs.DotOff+" offline", t.Muted)
		state = mascot.Sleeping
		headline = ui.SayFirst(ui.MomentAsleep)
	} else if len(r.Problems) > 0 || (r.Migrations != nil && !r.Migrations.UpToDate()) {
		state = mascot.Curious
		headline = "connected — a few things need a look."
	}

	var lines []string
	face := mascot.Face(t, state, 0)
	if face == "" {
		face = mascot.Glyph(t, state)
	}
	lines = append(lines, face+"  "+t.Text.Render(headline), "")

	var kv [][2]string
	if r.Online {
		db := "PostgreSQL " + r.Overview.Version
		if r.Runtime != nil {
			db += " " + t.Glyphs.Sep + " " + string(r.Runtime.Runtime)
		}
		db += " " + t.Glyphs.Sep + " " + r.Database.Host + ":" + itoa(int(r.Database.Port)) + " " + t.Glyphs.Sep + " " + ui.Bytes(r.Overview.Size)
		kv = append(kv, [2]string{"database", t.Text.Render(db)})
		schema := ui.Plural(int64(r.Tables), "table", "tables") + " " + t.Glyphs.Sep + " " + ui.Plural(int64(r.Indexes), "index", "indexes") + " " + t.Glyphs.Sep + " " + ui.Bytes(r.Bytes) + " of data"
		kv = append(kv, [2]string{"schema", t.Text.Render(schema)})
		if r.Migrations != nil {
			sum, tone := render.MigrationSummary(t, r.Migrations)
			kv = append(kv, [2]string{"migrations", t.Tone(tone).Render(t.Glyphs.Dot) + " " + t.Text.Render(sum)})
		}
		if len(r.Problems) == 0 {
			kv = append(kv, [2]string{"health", t.Success.Render(t.Glyphs.Insight) + " " + t.Text.Render("no problems")})
		} else {
			titles := make([]string, 0, len(r.Problems))
			for _, f := range r.Problems {
				titles = append(titles, f.Title)
			}
			kv = append(kv, [2]string{"health", t.Warning.Render(t.Glyphs.Warning) + " " + t.Text.Render(strings.Join(titles, "; "))})
		}
		activity := ui.Plural(int64(r.Overview.Connections), "connection", "connections")
		if r.Overview.CacheHit >= 0 {
			activity += " " + t.Glyphs.Sep + " cache hit " + ui.Percent(r.Overview.CacheHit)
		}
		activity += " " + t.Glyphs.Sep + " up " + ui.Duration(time.Since(r.Overview.StartedAt))
		kv = append(kv, [2]string{"activity", t.Muted.Render(activity)})
		kv = append(kv, [2]string{"url", t.Muted.Render(r.URL)})
	} else {
		where := r.Database.Host + ":" + itoa(int(r.Database.Port))
		state := "not reachable at " + where
		if r.Runtime != nil {
			switch {
			case !r.Runtime.Initialized:
				state = "not created yet " + t.Glyphs.Sep + " " + string(r.Runtime.Runtime)
			case !r.Runtime.Running:
				state = "stopped " + t.Glyphs.Sep + " " + string(r.Runtime.Runtime) + " " + t.Glyphs.Sep + " " + r.Runtime.Location
			}
		}
		kv = append(kv, [2]string{"database", t.Muted.Render(state)})
	}
	lines = append(lines, t.KV(ui.KV{Pairs: kv, KeyMin: 10}))

	if withNext {
		var next [][2]string
		if r.Online {
			if r.Migrations != nil && r.Migrations.Pending > 0 {
				next = append(next, [2]string{"nexus migration apply", "apply pending migrations"})
			}
			next = append(next, [2]string{"nexus", "open the live dashboard"}, [2]string{"nexus tables", "see your tables"}, [2]string{"nexus sql", "open the SQL shell"})
		} else if r.Env == "local" && (r.Runtime == nil || r.Runtime.Runtime != config.RuntimeExternal) {
			next = append(next, [2]string{"nexus up", "wake it up"})
		}
		if len(next) > 0 {
			var nl []string
			for _, n := range next {
				nl = append(nl, t.Code.Render(ui.PadRight(n[0], 23))+t.Muted.Render(n[1]))
			}
			lines = append(lines, "", strings.Join(nl, "\n"))
		}
	}
	return t.Panel(ui.Panel{Title: title, Right: badge, Body: strings.Join(lines, "\n"), Padding: 3})
}

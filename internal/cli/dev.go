package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/nothikemu/nexus/internal/render"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/project"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

type devResult struct {
	Runtime        config.Runtime `json:"runtime"`
	Version        string         `json:"version"`
	AlreadyRunning bool           `json:"already_running"`
	Created        bool           `json:"created"`
	Migrated       []string       `json:"migrations_applied"`
	Seeded         []string       `json:"seed_files"`
	Warnings       []string       `json:"warnings,omitempty"`
}

func newDevCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dev",
		Aliases: []string{"up", "start"},
		Short:   "start the local backend (alias: nexus up)",
		Long: "Starts the local PostgreSQL database for this project, applies pending migrations and " +
			"seeds a fresh database. The database keeps running in the background until `nexus dev stop`.",
		Example: "  nexus dev\n  nexus dev status\n  nexus dev logs -f\n  nexus dev stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDev(cmd.Context(), app)
		},
	}
	cmd.AddCommand(newDevStatusCmd(app), newDevStopCmd(app), newDevResetCmd(app), newDevLogsCmd(app))
	return cmd
}

func localRuntime(ctx context.Context, app *App) (*project.Project, localdb.Runtime, error) {
	if app.EnvName() != "local" {
		return nil, nil, &ui.Problem{Title: "nexus dev manages the local environment only.", Detail: "you selected " + app.EnvName() + ".", Exit: ui.ExitUsage}
	}
	p, err := app.Project()
	if err != nil {
		return nil, nil, err
	}
	rt, err := localdb.New(ctx, p)
	return p, rt, err
}

func runDev(ctx context.Context, app *App) error {
	p, rt, err := localRuntime(ctx, app)
	if err != nil {
		return err
	}
	t := app.T()
	out := devResult{Runtime: rt.Kind()}

	header := t.ToneGlyph(ui.ToneNexus) + " " + t.Title.Render("waking nexus up")
	if face := mascot.Face(t, mascot.Connecting, 0); face != "" {
		header = face + "  " + t.Title.Render("waking nexus up")
	}
	app.P.Block(header)

	if rt.Kind() != config.RuntimeExternal {
		task := app.P.TaskIndent("PostgreSQL", 4)
		res, err := rt.Start(ctx)
		if err != nil {
			task.Fail("")
			return err
		}
		out.Version, out.AlreadyRunning, out.Created, out.Warnings = res.Version, res.AlreadyRunning, res.Created, res.Warnings
		detail := fmt.Sprintf("%s %s %s %s 127.0.0.1:%d", res.Version, t.Glyphs.Sep, rt.Kind(), t.Glyphs.Sep, p.Config.Database.Port)
		switch {
		case res.AlreadyRunning:
			task.Done(detail + " " + t.Glyphs.Sep + " already running")
		case res.Created:
			task.Done(detail + " " + t.Glyphs.Sep + " fresh database")
		default:
			task.Done(detail)
		}
		for _, w := range res.Warnings {
			app.P.Line(ui.Indent(t.Warning.Render(t.Glyphs.Warning+" "+w), 6))
		}
	}

	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	eng, err := app.Engine(ctx)
	if err != nil {
		return err
	}
	task := app.P.TaskIndent("migrations", 4)
	if p.Config.Dev.ShouldAutoMigrate() {
		res, err := eng.Apply(ctx, migrate.ApplyOptions{}, nil)
		if err != nil {
			task.Fail("")
			return err
		}
		for _, a := range res.Applied {
			out.Migrated = append(out.Migrated, a.Migration.ID())
		}
		if len(res.Applied) == 0 {
			task.Done("up to date")
		} else {
			detail := ui.Plural(int64(len(res.Applied)), "applied", "applied")
			if res.Diff != nil && !res.Diff.Empty() {
				detail += " " + t.Glyphs.Sep + " " + render.DiffSummary(t, *res.Diff)
			}
			task.Done(detail)
		}
	} else {
		st, err := eng.Status(ctx)
		if err != nil {
			task.Fail("")
			return err
		}
		if st.Pending > 0 {
			task.Skip(ui.Plural(int64(st.Pending), "pending", "pending") + " " + t.Glyphs.Sep + " auto_migrate is off — run nexus migration apply")
		} else {
			task.Done("up to date")
		}
	}

	if out.Created && p.Config.Dev.ShouldAutoSeed() {
		files, err := p.SeedFiles()
		if err != nil {
			return err
		}
		if len(files) > 0 {
			task := app.P.TaskIndent("seed data", 4)
			seeded, err := runSeeds(ctx, app, pool, p, files)
			if err != nil {
				task.Fail("")
				return err
			}
			out.Seeded = seeded
			task.Done(ui.Plural(int64(len(seeded)), "file", "files"))
		}
	}

	if app.P.Mode().JSON {
		return app.P.JSON(out)
	}
	r, err := collectStatus(ctx, app)
	if err != nil {
		return err
	}
	app.P.Block(renderStatusPanel(t, r, true))
	return nil
}

// runSeeds executes seed files, each in its own transaction.
func runSeeds(ctx context.Context, app *App, pool *pgxpool.Pool, p *project.Project, files []string) ([]string, error) {
	var done []string
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return done, err
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, string(sql))
			return err
		})
		if err != nil {
			if pe, ok := asPg(err); ok {
				pr := app.sqlProblem(err, string(sql), 0, pe.Position)
				pr.Title = "seed file " + p.Rel(f) + " failed."
				return done, pr
			}
			return done, err
		}
		done = append(done, p.Rel(f))
	}
	return done, nil
}

func newDevStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show the local database process",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			p, rt, err := localRuntime(ctx, app)
			if err != nil {
				return err
			}
			kind, reason := localdb.Resolve(ctx, p)
			st, err := rt.Status(ctx)
			if err != nil {
				return err
			}
			if app.P.Mode().JSON {
				return app.P.JSON(st)
			}
			t := app.T()
			state := t.Muted.Render(t.Glyphs.DotOff + " stopped")
			if st.Running {
				state = t.Success.Render(t.Glyphs.Dot) + " " + t.Text.Render("running")
			} else if !st.Initialized {
				state = t.Muted.Render(t.Glyphs.DotOff + " not created yet")
			}
			pairs := [][2]string{
				{"state", state},
				{"runtime", t.Text.Render(string(kind)) + t.Muted.Render("  "+reason)},
			}
			if st.Version != "" {
				pairs = append(pairs, [2]string{"version", t.Text.Render(st.Version)})
			}
			if st.Port != 0 {
				pairs = append(pairs, [2]string{"port", t.Text.Render(itoa(st.Port))})
			}
			if st.PID != 0 {
				pairs = append(pairs, [2]string{"pid", t.Text.Render(itoa(st.PID))})
			}
			if st.Location != "" {
				pairs = append(pairs, [2]string{"data", t.Muted.Render(st.Location)})
			}
			head := "the local database is running."
			tone := ui.ToneNexus
			if !st.Running {
				head, tone = ui.SayFirst(ui.MomentAsleep), ui.ToneInfo
			}
			app.P.Say(tone, head, t.KV(ui.KV{Pairs: pairs}))
			return nil
		},
	}
}

func newDevStopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "stop the local database (your data is kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, rt, err := localRuntime(ctx, app)
			if err != nil {
				return err
			}
			task := app.P.Task("PostgreSQL")
			err = rt.Stop(ctx)
			if errors.Is(err, localdb.ErrNotRunning) {
				task.Skip("wasn't running")
			} else if err != nil {
				task.Fail("")
				return err
			} else {
				task.Done("stopped " + app.T().Glyphs.Sep + " data kept")
			}
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]bool{"stopped": err == nil})
			}
			t := app.T()
			face := mascot.Face(t, mascot.Sleeping, 0)
			if face == "" {
				face = mascot.Glyph(t, mascot.Sleeping)
			}
			app.P.Block(face + "  " + t.Text.Render(ui.Say(ui.MomentGoodbye)) + "\n\n" + t.HintString("wake it with nexus up"))
			return nil
		},
	}
}

func newDevResetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "delete the local database and start fresh",
		Long:  "Stops the local database, deletes all of its data, then recreates it, applies every migration and seeds it.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			p, rt, err := localRuntime(ctx, app)
			if err != nil {
				return err
			}
			if rt.Kind() == config.RuntimeExternal {
				return localdb.ErrExternal
			}
			op := safety.Operation{
				Level:  safety.Destructive,
				Action: "reset the local database for " + p.Name(),
				Details: []string{
					"every table and row in " + p.Config.Database.Name + " is deleted",
					"migrations and seeds run again on a fresh database",
				},
			}
			if err := app.Allow(ctx, op); err != nil {
				return err
			}
			task := app.P.Task("delete local data")
			if err := rt.Destroy(ctx); err != nil {
				task.Fail("")
				return err
			}
			task.Done(string(rt.Kind()))
			return runDev(ctx, app)
		},
	}
}

func newDevLogsCmd(app *App) *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:     "logs",
		Short:   "show the local database's logs",
		Example: "  nexus dev logs\n  nexus dev logs -f\n  nexus dev logs -n 200",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, rt, err := localRuntime(ctx, app)
			if err != nil {
				return err
			}
			var w io.Writer = app.P.Out
			if !app.T().Mode.Plain && !app.P.Mode().JSON {
				lw := &logWriter{t: app.T(), out: app.P.Out}
				defer lw.Flush()
				w = lw
			}
			return rt.Logs(ctx, w, lines, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines to show")
	return cmd
}

// logWriter colours PostgreSQL log lines by severity.
type logWriter struct {
	t   *ui.Theme
	out io.Writer
	buf []byte
}

var pgLogRe = regexp.MustCompile(`^(\d{4}-\d\d-\d\d) (\d\d:\d\d:\d\d(?:\.\d+)?) \S+ \[(\d+)\] (\w+):\s+(.*)$`)

func (w *logWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			return len(b), nil
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if _, err := io.WriteString(w.out, w.format(line)+"\n"); err != nil {
			return len(b), err
		}
	}
}

// Flush writes any trailing partial line.
func (w *logWriter) Flush() {
	if len(w.buf) > 0 {
		_, _ = io.WriteString(w.out, w.format(string(w.buf))+"\n")
		w.buf = nil
	}
}

func (w *logWriter) format(line string) string {
	t := w.t
	m := pgLogRe.FindStringSubmatch(line)
	if m == nil {
		return t.Muted.Render(line)
	}
	level, msg := m[4], m[5]
	style := t.Text
	lvl := t.Faint
	switch level {
	case "ERROR", "FATAL", "PANIC":
		style, lvl = t.Error, t.Error.Bold(true)
	case "WARNING":
		style, lvl = t.Warning, t.Warning.Bold(true)
	case "STATEMENT", "DETAIL", "HINT", "CONTEXT":
		style = t.Muted
	}
	return t.Faint.Render(m[2]) + "  " + lvl.Render(ui.PadRight(strings.ToLower(level), 10)) + style.Render(msg)
}

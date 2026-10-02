package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/nothikemu/nexus/internal/render"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/localdb"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
	"github.com/nothikemu/nexus/internal/project"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

// checkState is the outcome of a doctor check.
type checkState string

const (
	checkOK   checkState = "ok"
	checkWarn checkState = "warn"
	checkFail checkState = "fail"
	checkSkip checkState = "skip"
)

type check struct {
	Name   string     `json:"name"`
	State  checkState `json:"state"`
	Detail string     `json:"detail"`
	Hint   string     `json:"hint,omitempty"`
}

func newDoctorCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "check your setup",
		Long:  "Checks the project configuration, PostgreSQL and Docker availability, the database connection, migrations, schema health, local secrets and the terminal.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), app)
		},
	}
}

func runDoctor(ctx context.Context, app *App) error {
	t := app.T()
	if !app.P.Quiet() {
		app.P.Say(ui.ToneNexus, "nexus is checking things over")
	}
	var checks []check
	run := func(name string, fn func() check) {
		task := app.P.TaskIndent(name, 4)
		c := fn()
		c.Name = name
		checks = append(checks, c)
		switch c.State {
		case checkOK:
			task.Done(c.Detail)
		case checkWarn:
			task.Warn(c.Detail)
		case checkFail:
			task.Fail(c.Detail)
		default:
			task.Skip(c.Detail)
		}
		if c.Hint != "" && c.State != checkOK && !app.P.Quiet() {
			app.P.Line(ui.Indent(t.Muted.Render(t.Glyphs.Arrow+" "+c.Hint), 6))
		}
	}

	p, projErr := app.Project()
	run("project", func() check {
		switch {
		case errors.Is(projErr, project.ErrNotFound):
			return check{State: checkSkip, Detail: "not inside a project", Hint: "nexus init my-app"}
		case projErr != nil:
			return check{State: checkFail, Detail: projErr.Error()}
		case len(p.Warnings) > 0:
			return check{State: checkWarn, Detail: strings.Join(p.Warnings, "; ")}
		}
		return check{State: checkOK, Detail: p.Name() + " " + t.Glyphs.Sep + " " + p.Rel(p.ConfigPath)}
	})

	bins := localdb.FindBinaries(ctx)
	run("postgresql", func() check {
		if len(bins) == 0 {
			return check{State: checkWarn, Detail: "no local installation found", Hint: "only needed for runtime: native"}
		}
		var vs []string
		for _, b := range bins {
			vs = append(vs, b.Version)
		}
		c := check{State: checkOK, Detail: strings.Join(vs, ", ") + " " + t.Glyphs.Sep + " " + bins[0].Dir}
		if p != nil {
			want := p.Config.Database.Version
			found := false
			for _, b := range bins {
				found = found || b.Major == want
			}
			if !found {
				c.State, c.Hint = checkWarn, fmt.Sprintf("nexus.yaml asks for PostgreSQL %d", want)
			}
		}
		return c
	})

	run("docker", func() check {
		if p == nil {
			return check{State: checkSkip, Detail: "no project"}
		}
		rt, _ := localdb.Resolve(ctx, p)
		err := dockerCheck(ctx, p)
		switch {
		case err == nil:
			return check{State: checkOK, Detail: "daemon reachable"}
		case rt == config.RuntimeDocker:
			return check{State: checkFail, Detail: err.Error(), Hint: "start Docker, or switch database.runtime"}
		default:
			return check{State: checkSkip, Detail: "not available " + t.Glyphs.Sep + " not needed"}
		}
	})

	var target *Target
	run("runtime", func() check {
		if p == nil && app.Flags.DBURL == "" {
			return check{State: checkSkip, Detail: "no project"}
		}
		var err error
		target, err = app.Target(ctx)
		if err != nil {
			return check{State: checkFail, Detail: app.problem(err).Title}
		}
		if target.Local == nil {
			return check{State: checkOK, Detail: target.Env + " " + t.Glyphs.Sep + " " + pg.Describe(target.URL).String()}
		}
		kind, reason := localdb.Resolve(ctx, p)
		if err := target.Local.Check(ctx); err != nil {
			pr := app.problem(err)
			return check{State: checkFail, Detail: string(kind) + " " + t.Glyphs.Sep + " " + pr.Title, Hint: pr.Hint}
		}
		return check{State: checkOK, Detail: string(kind) + " " + t.Glyphs.Sep + " " + reason}
	})

	var online bool
	run("database", func() check {
		if target == nil {
			return check{State: checkSkip, Detail: "no target"}
		}
		pool, err := app.DB(ctx)
		if err != nil {
			pr := app.problem(err)
			state := checkFail
			if pr.Exit == ui.ExitUnreachable && target.Env == "local" {
				state = checkWarn
			}
			return check{State: state, Detail: pr.Title + " " + pr.Detail, Hint: pr.Hint}
		}
		online = true
		start := time.Now()
		v, num, err := pg.ServerVersion(ctx, pool)
		if err != nil {
			return check{State: checkFail, Detail: err.Error()}
		}
		c := check{State: checkOK, Detail: "PostgreSQL " + v + " " + t.Glyphs.Sep + " " + ui.Duration(time.Since(start)) + " round trip"}
		if p != nil && target.Env == "local" && num/10000 != p.Config.Database.Version && target.Local != nil && target.Local.Kind() != config.RuntimeExternal {
			c.State = checkWarn
			c.Hint = fmt.Sprintf("nexus.yaml asks for %d; recreate with nexus dev reset to switch", p.Config.Database.Version)
		}
		return c
	})

	run("migrations", func() check {
		if !online || p == nil || app.Flags.DBURL != "" {
			return check{State: checkSkip, Detail: "needs a project database"}
		}
		eng, err := app.Engine(ctx)
		if err != nil {
			return check{State: checkFail, Detail: err.Error()}
		}
		st, err := eng.Status(ctx)
		if err != nil {
			return check{State: checkFail, Detail: app.problem(err).Title}
		}
		sum, _ := render.MigrationSummary(t, st)
		switch {
		case st.Modified > 0 || st.Missing > 0:
			return check{State: checkFail, Detail: sum, Hint: "nexus migration status"}
		case st.Ordering > 0:
			return check{State: checkWarn, Detail: sum + " " + t.Glyphs.Sep + " out of order", Hint: "nexus migration apply --allow-out-of-order"}
		case st.Pending > 0:
			return check{State: checkWarn, Detail: sum, Hint: "nexus migration apply"}
		}
		return check{State: checkOK, Detail: sum}
	})

	run("schema health", func() check {
		if !online {
			return check{State: checkSkip, Detail: "database offline"}
		}
		pool, _ := app.DB(ctx)
		schemas, err := app.Schemas(ctx)
		if err != nil {
			return check{State: checkFail, Detail: err.Error()}
		}
		snap, err := introspect.Load(ctx, pool, schemas)
		if err != nil {
			return check{State: checkFail, Detail: err.Error()}
		}
		findings := stats.Health(snap)
		if len(findings) == 0 {
			return check{State: checkOK, Detail: ui.Plural(int64(len(snap.BaseTables())), "table", "tables") + " " + t.Glyphs.Sep + " no problems"}
		}
		var titles []string
		for _, f := range findings {
			titles = append(titles, f.Title)
		}
		return check{State: checkWarn, Detail: strings.Join(titles, "; "), Hint: "details: nexus db inspect"}
	})

	run("secrets", func() check {
		if p == nil {
			return check{State: checkSkip, Detail: "no project"}
		}
		return secretsCheck(p)
	})

	run("terminal", func() check {
		m := app.P.Mode()
		var parts []string
		switch {
		case m.JSON:
			parts = append(parts, "json")
		case m.Plain:
			parts = append(parts, "plain")
		case m.Color:
			parts = append(parts, strings.ToLower(colorProfileName(t)))
		default:
			parts = append(parts, "no colour")
		}
		if m.Animate {
			parts = append(parts, "animated")
		}
		if m.Interactive {
			parts = append(parts, "interactive")
		}
		parts = append(parts, fmt.Sprintf("%d columns", m.Width))
		return check{State: checkOK, Detail: strings.Join(parts, " "+t.Glyphs.Sep+" ")}
	})

	var warn, fail int
	for _, c := range checks {
		switch c.State {
		case checkWarn:
			warn++
		case checkFail:
			fail++
		}
	}
	if app.P.Mode().JSON {
		if err := app.P.JSON(map[string]any{"checks": checks, "warnings": warn, "failures": fail}); err != nil {
			return err
		}
	} else {
		state := mascot.Success
		head := ui.SayFirst(ui.MomentAllGood) + " " + ui.Plural(int64(len(checks)-warn-fail), "check passed.", "checks passed.")
		tone := ui.ToneSuccess
		if warn+fail > 0 {
			state, tone = mascot.Curious, ui.ToneWarning
			head = ui.Plural(int64(warn+fail), "thing needs", "things need") + " attention."
		}
		if fail > 0 {
			state, tone = mascot.Warning, ui.ToneError
		}
		face := mascot.Face(t, state, 0)
		if face == "" {
			app.P.Say(tone, head)
		} else {
			app.P.Block(face + "  " + t.Tone(tone).Bold(true).Render(head))
		}
	}
	if fail > 0 {
		return &ui.Problem{Title: "doctor found problems.", Exit: ui.ExitFailure, Code: "doctor_failed"}
	}
	return nil
}

func dockerCheck(ctx context.Context, p *project.Project) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg := *p.Config
	cfg.Database.Runtime = config.RuntimeDocker
	clone := *p
	clone.Config = &cfg
	rt, err := localdb.New(c, &clone)
	if err != nil {
		return err
	}
	return rt.Check(c)
}

// secretsCheck makes sure local credentials can't be committed or read by others.
func secretsCheck(p *project.Project) check {
	path := filepath.Join(p.StateDir(), "local.json")
	gi, err := os.ReadFile(p.Path(".gitignore"))
	_, inGit := os.Stat(p.Path(".git"))
	if inGit == nil && (err != nil || !project.IgnoresStateDir(string(gi))) {
		return check{State: checkFail, Detail: project.StateDirName + "/ isn't git-ignored — local credentials could be committed", Hint: "add " + project.StateDirName + "/ to .gitignore"}
	}
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return check{State: checkOK, Detail: "no local credentials yet"}
	}
	if err != nil {
		return check{State: checkWarn, Detail: err.Error()}
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return check{State: checkWarn, Detail: fmt.Sprintf("%s is readable by others (%v)", p.Rel(path), fi.Mode().Perm()), Hint: "chmod 600 " + p.Rel(path)}
	}
	return check{State: checkOK, Detail: "local credentials are private and git-ignored"}
}

func colorProfileName(t *ui.Theme) string {
	switch t.R.ColorProfile() {
	case termenv.TrueColor:
		return "truecolor"
	case termenv.ANSI256:
		return "256 colours"
	case termenv.ANSI:
		return "16 colours"
	default:
		return "no colour"
	}
}

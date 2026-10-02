package cli

import (
	"github.com/nothikemu/nexus/internal/render"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/ui"
)

func newMigrationCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "migration",
		Aliases: []string{"migrations", "migrate"},
		Short:   "create, preview and apply schema migrations",
		Long: "Migrations are SQL files in migrations/, named <timestamp>_<name>.sql, with `-- nexus:up` and " +
			"`-- nexus:down` sections. Each runs in its own transaction under a lock, and every applied migration is " +
			"checksummed so edits after the fact are caught.",
		Example: "  nexus migration create add_profiles\n  nexus migration diff\n  nexus migration apply\n  nexus migration rollback",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrationStatus(cmd, app)
		},
	}
	cmd.AddCommand(
		newMigrationCreateCmd(app),
		&cobra.Command{
			Use:   "status",
			Short: "list migrations and whether they're applied",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return runMigrationStatus(cmd, app) },
		},
		newMigrationApplyCmd(app),
		newMigrationDiffCmd(app),
		newMigrationRollbackCmd(app),
		newMigrationRepairCmd(app),
	)
	return cmd
}

func newMigrationCreateCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "create <name>",
		Short:   "create a new migration file",
		Example: "  nexus migration create add_profiles\n  nexus migration create \"index users by email\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := app.Project()
			if err != nil {
				return err
			}
			name := args[0]
			for _, a := range args[1:] {
				name += " " + a
			}
			path, err := migrate.Create(p.MigrationsDir(), name, time.Now())
			if err != nil {
				return err
			}
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]string{"path": p.Rel(path)})
			}
			t := app.T()
			app.P.Say(ui.ToneSuccess, "created "+p.Rel(path),
				t.Muted.Render("write your change under ")+t.Code.Render("-- nexus:up")+t.Muted.Render(" and how to undo it under ")+t.Code.Render("-- nexus:down")+t.Muted.Render("."),
				t.Code.Render("nexus migration diff")+t.Muted.Render("    preview what it changes")+"\n"+t.Code.Render("nexus migration apply")+t.Muted.Render("   apply it"))
			return nil
		},
	}
}

func runMigrationStatus(cmd *cobra.Command, app *App) error {
	ctx := cmd.Context()
	eng, err := app.Engine(ctx)
	if err != nil {
		return err
	}
	st, err := eng.Status(ctx)
	if err != nil {
		return err
	}
	if app.P.Mode().JSON {
		return app.P.JSON(st)
	}
	t := app.T()
	sum, tone := render.MigrationSummary(t, st)
	app.P.Say(tone, sum, render.MigrationStatus(t, st))
	if st.Pending > 0 && st.Modified == 0 && st.Missing == 0 {
		app.P.Hint("preview with nexus migration diff · apply with nexus migration apply")
	} else if st.Modified > 0 || st.Missing > 0 {
		app.P.Hint("restore the files, or accept them with nexus migration repair")
	}
	return nil
}

// progress shows a task per migration while it runs.
type progress struct {
	app   *App
	tasks map[string]*ui.Task
	verb  string
}

func (p *progress) Started(m *migrate.Migration) {
	if p.tasks == nil {
		p.tasks = map[string]*ui.Task{}
	}
	p.tasks[m.Version] = p.app.P.TaskIndent(m.Name, 4)
}

func (p *progress) Finished(m *migrate.Migration, _ time.Duration, err error) {
	task := p.tasks[m.Version]
	if task == nil {
		return
	}
	if err != nil {
		task.Fail(m.Version)
	} else {
		task.Done(m.Version)
	}
}

func newMigrationApplyCmd(app *App) *cobra.Command {
	var opts migrate.ApplyOptions
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "apply pending migrations",
		Long: "Applies pending migrations in order, each in its own transaction. If one fails, it is rolled " +
			"back completely and nothing after it runs. With --dry-run, everything runs in a transaction that is " +
			"rolled back, and Nexus reports the schema changes it would make.",
		Example: "  nexus migration apply\n  nexus migration apply --dry-run\n  nexus migration apply --to 20261002141500\n  nexus migration apply --env production",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApply(cmd, app, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "run inside a transaction, report changes, then roll back")
	cmd.Flags().StringVar(&opts.Target, "to", "", "apply up to and including this version")
	cmd.Flags().BoolVar(&opts.AllowOutOfOrder, "allow-out-of-order", false, "apply pending migrations older than the latest applied one")
	return cmd
}

func newMigrationDiffCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "preview the schema changes pending migrations would make",
		Long: "Runs pending migrations inside a transaction, compares the schema before and after, and rolls " +
			"everything back. Nothing is changed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApply(cmd, app, migrate.ApplyOptions{DryRun: true, AllowOutOfOrder: true})
		},
	}
}

func runApply(cmd *cobra.Command, app *App, opts migrate.ApplyOptions) error {
	ctx := cmd.Context()
	eng, err := app.Engine(ctx)
	if err != nil {
		return err
	}
	t := app.T()
	st, err := eng.Status(ctx)
	if err != nil {
		return err
	}
	plan, err := eng.Plan(st, opts)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		if app.P.Mode().JSON {
			return app.P.JSON(migrate.ApplyResult{Applied: []migrate.AppliedMigration{}, DryRun: opts.DryRun})
		}
		app.P.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentNothingNew), t.Muted.Render(ui.Plural(int64(st.Applied), "migration applied", "migrations applied")+", none pending."))
		return nil
	}
	if !opts.DryRun {
		details := make([]string, len(plan))
		for i, m := range plan {
			details[i] = m.ID()
		}
		op := safety.Operation{Level: safety.SafeWrite, Action: "apply " + ui.Plural(int64(len(plan)), "migration", "migrations"), Details: details}
		if err := app.Allow(ctx, op); err != nil {
			return err
		}
	}

	title := "applying " + ui.Plural(int64(len(plan)), "migration", "migrations")
	if opts.DryRun {
		title = "previewing " + ui.Plural(int64(len(plan)), "migration", "migrations") + " (nothing will be saved)"
	}
	app.P.Say(ui.ToneNexus, title)
	res, err := eng.Apply(ctx, opts, &progress{app: app})
	if err != nil {
		return err
	}
	if app.P.Mode().JSON {
		return app.P.JSON(res)
	}
	for _, a := range res.Applied {
		if a.Skipped != "" {
			app.P.Line(ui.Indent(t.Muted.Render(t.Glyphs.Skip+" "+a.Migration.Name+"  "+a.Skipped), 4))
		}
	}
	var body []string
	summary := ui.Plural(int64(len(res.Applied)), "migration", "migrations")
	if res.Diff != nil {
		body = append(body, render.Diff(t, *res.Diff))
		summary += " " + t.Glyphs.Sep + " " + render.DiffSummary(t, *res.Diff)
	}
	if opts.DryRun {
		app.P.Say(ui.ToneSuccess, "preview complete — nothing was changed.", append([]string{t.Muted.Render(summary)}, body...)...)
		app.P.Hint("apply for real with nexus migration apply")
		return nil
	}
	app.P.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentNice)+" "+ui.Plural(int64(len(res.Applied)), "migration applied.", "migrations applied."), append([]string{t.Muted.Render(summary + " " + t.Glyphs.Sep + " 0 errors")}, body...)...)
	return nil
}

func newMigrationRollbackCmd(app *App) *cobra.Command {
	var steps int
	cmd := &cobra.Command{
		Use:     "rollback",
		Short:   "revert the most recently applied migrations",
		Long:    "Runs the `-- nexus:down` section of the most recently applied migrations, newest first, each in its own transaction.",
		Example: "  nexus migration rollback\n  nexus migration rollback --steps 3",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			eng, err := app.Engine(ctx)
			if err != nil {
				return err
			}
			st, err := eng.Status(ctx)
			if err != nil {
				return err
			}
			plan, err := migrate.RollbackPlan(st, steps)
			if err != nil {
				return err
			}
			if len(plan) == 0 {
				if app.P.Mode().JSON {
					return app.P.JSON(migrate.RollbackResult{Reverted: []migrate.AppliedMigration{}})
				}
				app.P.Say(ui.ToneInfo, "nothing to roll back.", "no migrations have been applied.")
				return nil
			}
			details := make([]string, len(plan))
			for i, e := range plan {
				details[i] = e.Version + "_" + e.Name
			}
			op := safety.Operation{Level: safety.Destructive, Action: "roll back " + ui.Plural(int64(len(plan)), "migration", "migrations"), Details: details}
			if err := app.Allow(ctx, op); err != nil {
				return err
			}
			app.P.Say(ui.ToneNexus, "rolling back "+ui.Plural(int64(len(plan)), "migration", "migrations"))
			res, err := eng.Rollback(ctx, steps, &progress{app: app})
			if err != nil {
				return err
			}
			if app.P.Mode().JSON {
				return app.P.JSON(res)
			}
			t := app.T()
			var body []string
			if res.Diff != nil {
				body = append(body, t.Muted.Render(render.DiffSummary(t, *res.Diff)), render.Diff(t, *res.Diff))
			}
			app.P.Say(ui.ToneSuccess, "rolled back "+ui.Plural(int64(len(res.Reverted)), "migration.", "migrations."), body...)
			return nil
		},
	}
	cmd.Flags().IntVar(&steps, "steps", 1, "how many migrations to revert")
	return cmd
}

func newMigrationRepairCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "reconcile migration history with the files on disk",
		Long: "Accepts the current contents of migrations that were edited after being applied, and forgets " +
			"history for migrations whose files were deleted. Only Nexus's history changes — never your schema.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			eng, err := app.Engine(ctx)
			if err != nil {
				return err
			}
			st, err := eng.Status(ctx)
			if err != nil {
				return err
			}
			plan := migrate.RepairPlan(st)
			if len(plan) == 0 {
				if app.P.Mode().JSON {
					return app.P.JSON(map[string]any{"repaired": []migrate.RepairAction{}})
				}
				app.P.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentSynced), "migration history matches the files on disk.")
				return nil
			}
			details := make([]string, len(plan))
			for i, a := range plan {
				what := "accept the edited file"
				if a.Action == "forget" {
					what = "forget (file deleted)"
				}
				details[i] = a.Entry.Version + "_" + a.Entry.Name + " — " + what
			}
			// Rewriting history deserves a question even locally.
			op := safety.Operation{Level: safety.SafeWrite, AlwaysAsk: true, Action: "repair migration history (" + strconv.Itoa(len(plan)) + ")", Details: details}
			if err := app.Allow(ctx, op); err != nil {
				return err
			}
			if err := eng.Repair(ctx, plan); err != nil {
				return err
			}
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]any{"repaired": plan})
			}
			app.P.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentBetter), ui.Plural(int64(len(plan)), "history entry repaired.", "history entries repaired."))
			return nil
		},
	}
}

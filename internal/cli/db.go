package cli

import (
	"context"
	"fmt"
	"github.com/nothikemu/nexus/internal/render"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/explain"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/ui"
)

func newDBCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "db",
		Short:   "inspect and maintain the database",
		Example: "  nexus db inspect\n  nexus db tables\n  nexus db schema\n  nexus db explain \"select * from users where email = 'a@b.c'\"",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInspect(cmd.Context(), app)
		},
	}
	cmd.AddCommand(
		newDBShellCmd(app),
		&cobra.Command{
			Use:   "inspect",
			Short: "overview of the database and its health",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return runInspect(cmd.Context(), app) },
		},
		newDBTablesCmd(app),
		newDBSchemaCmd(app),
		newExplainCmd(app, "explain <sql>", "show how PostgreSQL runs a query", false),
		newMaintenanceCmd(app, "vacuum"),
		newMaintenanceCmd(app, "analyze"),
		newDBSeedCmd(app),
		newDBURLCmd(app),
	)
	return cmd
}

func newDBShellCmd(app *App) *cobra.Command {
	var builtin bool
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "open a SQL shell (psql when installed)",
		Long:  "Opens psql connected to the target database when psql is installed, otherwise the Nexus SQL shell.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			t, err := app.Target(ctx)
			if err != nil {
				return err
			}
			psql, lookErr := exec.LookPath("psql")
			if builtin || lookErr != nil {
				return runREPL(ctx, app)
			}
			if _, err := app.DB(ctx); err != nil { // fail with a friendly message before handing over
				return err
			}
			app.Close()
			if runtime.GOOS == "windows" {
				c := exec.CommandContext(ctx, psql, t.URL)
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				return c.Run()
			}
			return syscall.Exec(psql, []string{"psql", t.URL}, os.Environ())
		},
	}
	cmd.Flags().BoolVar(&builtin, "builtin", false, "use the Nexus SQL shell even if psql is installed")
	return cmd
}

type inspectReport struct {
	Overview *stats.Overview `json:"overview"`
	Tables   int             `json:"tables"`
	Views    int             `json:"views"`
	Indexes  int             `json:"indexes"`
	Problems []stats.Finding `json:"problems"`
}

func runInspect(ctx context.Context, app *App) error {
	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	ov, err := stats.LoadOverview(ctx, pool)
	if err != nil {
		return err
	}
	snap, err := app.Snapshot(ctx)
	if err != nil {
		return err
	}
	r := inspectReport{Overview: ov, Tables: len(snap.BaseTables()), Indexes: snap.IndexCount(), Problems: stats.Health(snap)}
	for _, tb := range snap.Tables {
		if tb.IsView() {
			r.Views++
		}
	}
	if r.Problems == nil {
		r.Problems = []stats.Finding{}
	}
	if app.P.Mode().JSON {
		return app.P.JSON(r)
	}
	t := app.T()
	var exts []string
	for _, e := range snap.Extensions {
		exts = append(exts, e.Name)
	}
	cache := "no reads yet"
	if ov.CacheHit >= 0 {
		cache = ui.Percent(ov.CacheHit)
	}
	pairs := [][2]string{
		{"size", ui.Bytes(ov.Size)},
		{"objects", t.JoinSep(ui.Plural(int64(r.Tables), "table", "tables"), ui.Plural(int64(r.Indexes), "index", "indexes"), ui.Plural(int64(r.Views), "view", "views"), ui.Plural(int64(len(snap.Functions)), "function", "functions"))},
		{"connections", fmt.Sprintf("%d / %d %s %d active", ov.Connections, ov.MaxConnections, t.Glyphs.Sep, ov.Active)},
		{"cache hit", cache},
		{"transactions", ui.Count(ov.Commits) + " committed " + t.Glyphs.Sep + " " + ui.Count(ov.Rollbacks) + " rolled back"},
		{"uptime", ui.Duration(time.Since(ov.StartedAt))},
		{"extensions", strings.Join(exts, ", ")},
		{"schemas", strings.Join(snap.Schemas, ", ")},
	}
	if ov.InRecovery {
		pairs = append(pairs, [2]string{"role", "read replica (in recovery)"})
	}
	for i := range pairs {
		pairs[i][1] = t.Text.Render(pairs[i][1])
	}
	app.P.Say(ui.ToneNexus, ov.Database+" on PostgreSQL "+ov.Version, t.KV(ui.KV{Pairs: pairs}))
	if len(r.Problems) == 0 {
		app.P.Say(ui.ToneSuccess, ui.SayFirst(ui.MomentAllGood), t.Muted.Render("no schema problems found."))
		return nil
	}
	app.P.Say(ui.ToneNexus, fmt.Sprintf("nexus found %s.", ui.Plural(int64(len(r.Problems)), "thing worth a look", "things worth a look")), render.Findings(t, r.Problems, 0, true))
	return nil
}

func newDBTablesCmd(app *App) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "tables",
		Short:   "list tables",
		Example: "  nexus db tables\n  nexus db tables --all   # include views and partitions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap, err := app.Snapshot(cmd.Context())
			if err != nil {
				return err
			}
			var list []*introspect.Table
			for _, tb := range snap.Tables {
				if all || ((tb.Kind == introspect.KindTable || tb.Kind == introspect.KindPartitioned) && tb.PartitionOf == "") {
					list = append(list, tb)
				}
			}
			if app.P.Mode().JSON {
				if list == nil {
					list = []*introspect.Table{}
				}
				return app.P.JSON(list)
			}
			t := app.T()
			if len(list) == 0 {
				app.P.Say(ui.ToneInfo, "no tables yet.", t.Muted.Render("create one in a migration: ")+t.Cmd("nexus migration create create_users"))
				return nil
			}
			var total int64
			for _, tb := range list {
				total += tb.Bytes
			}
			app.P.Say(ui.ToneNexus, t.JoinSep(ui.Plural(int64(len(list)), "table", "tables"), ui.Bytes(total)), render.Tables(t, list))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include views, materialized views and partitions")
	return cmd
}

func newDBSchemaCmd(app *App) *cobra.Command {
	var columns bool
	cmd := &cobra.Command{
		Use:     "schema [table]",
		Short:   "show the database structure as a tree",
		Example: "  nexus db schema\n  nexus db schema --columns\n  nexus db schema users",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) == 1 {
				_, tb, err := loadTable(ctx, app, args[0])
				if err != nil {
					return err
				}
				if app.P.Mode().JSON {
					return app.P.JSON(tb)
				}
				app.P.Block(render.TableHeadline(app.T(), tb) + "\n\n" + render.Columns(app.T(), tb, 2))
				return nil
			}
			snap, err := app.Snapshot(ctx)
			if err != nil {
				return err
			}
			if app.P.Mode().JSON {
				return app.P.JSON(snap)
			}
			t := app.T()
			app.P.Say(ui.ToneNexus, "schema", ui.Indent(render.SchemaTree(t, snap, columns), 0))
			if len(snap.Extensions) > 0 {
				var exts []string
				for _, e := range snap.Extensions {
					exts = append(exts, e.Name+t.Muted.Render(" "+e.Version))
				}
				app.P.Line("  " + t.Key.Render("extensions  ") + strings.Join(exts, t.Faint.Render(" · ")))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&columns, "columns", "c", false, "include every column")
	return cmd
}

func newExplainCmd(app *App, use, short string, defaultAnalyze bool) *cobra.Command {
	var analyze bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long: "Shows the query plan PostgreSQL chooses, where the time goes, and what might make it faster. " +
			"With --analyze the query really runs — inside a transaction that is always rolled back, so writes leave no trace.",
		Example: "  nexus db explain \"select * from users where email = 'ada@example.com'\"\n  nexus query analyze \"select * from orders order by total desc limit 10\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(cmd.Context(), app, strings.Join(args, " "), analyze || defaultAnalyze)
		},
	}
	if !defaultAnalyze {
		cmd.Flags().BoolVarP(&analyze, "analyze", "a", false, "run the query and measure it (in a rolled-back transaction)")
	}
	return cmd
}

func runExplain(ctx context.Context, app *App, sql string, analyze bool) error {
	t, err := app.Target(ctx)
	if err != nil {
		return err
	}
	if analyze && t.Protected {
		if level, reasons := safety.ClassifySQL(sql); level > safety.ReadOnly {
			op := safety.Operation{Level: safety.SafeWrite, Action: "execute this statement for EXPLAIN ANALYZE (rolled back afterwards)", Details: reasons}
			if err := app.Allow(ctx, op); err != nil {
				return err
			}
		}
	}
	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	task := app.P.Task("thinking")
	plan, err := explain.Run(ctx, conn.Conn(), sql, analyze)
	if err != nil {
		task.Clear()
		if pe, ok := pg.AsPgError(err); ok {
			// EXPLAIN prefixes the statement, so shift the position back.
			pos := pe.Position
			if pos > 0 {
				prefix := len([]rune("explain (format json) "))
				if analyze {
					prefix = len([]rune("explain (analyze, buffers, format json) "))
				}
				pos -= int32(prefix)
			}
			return app.sqlProblem(err, strings.TrimSpace(sql), 0, pos)
		}
		return err
	}
	task.Clear()
	snap, err := app.Snapshot(ctx)
	if err != nil {
		return err
	}
	insights := explain.Analyze(plan, snap)
	if app.P.Mode().JSON {
		return app.P.JSON(map[string]any{"plan": plan, "totals": plan.Totals(), "insights": insights})
	}
	th := app.T()
	timing := ""
	if plan.Analyzed {
		timing = "planning " + ui.Millis(plan.PlanningMs) + " " + th.Glyphs.Sep + " execution " + ui.Millis(plan.ExecutionMs)
	} else {
		timing = "estimated " + th.Glyphs.Sep + " not executed"
	}
	app.P.Say(ui.ToneSuccess, "query plan", th.Muted.Render(timing), render.Plan(th, plan), render.PlanSummary(th, plan))

	var warnings []explain.Insight
	for _, in := range insights {
		if in.Severity != explain.Good {
			warnings = append(warnings, in)
		}
	}
	switch {
	case len(warnings) > 0 && warnings[0].Severity == explain.Warning:
		app.P.Say(ui.ToneNexus, ui.SayFirst(ui.MomentNoticed), render.Insights(th, insights))
	case len(insights) > 0:
		app.P.Block(render.Insights(th, insights))
	}
	return nil
}

func newMaintenanceCmd(app *App, verb string) *cobra.Command {
	var full, withAnalyze bool
	short := "reclaim space and refresh statistics"
	long := "Runs VACUUM on one table, or on every table you own. VACUUM FULL rewrites the table to return space to the operating system, but locks it while it runs."
	if verb == "analyze" {
		short = "refresh planner statistics"
		long = "Runs ANALYZE so the query planner has up-to-date statistics."
	}
	cmd := &cobra.Command{
		Use:   verb + " [table]",
		Short: short,
		Long:  long,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pool, err := app.DB(ctx)
			if err != nil {
				return err
			}
			target, label := "", "database"
			var sizeSQL string
			var sizeArgs []any
			if len(args) == 1 {
				_, tb, err := loadTable(ctx, app, args[0])
				if err != nil {
					return err
				}
				target = " " + pg.QuoteIdent(tb.Schema, tb.Name)
				label = tb.DisplayName()
				sizeSQL, sizeArgs = "select pg_total_relation_size($1::regclass)", []any{pg.QuoteIdent(tb.Schema, tb.Name)}
			} else {
				sizeSQL = "select pg_database_size(current_database())"
			}
			var opts []string
			if full {
				opts = append(opts, "full")
			}
			if withAnalyze && verb == "vacuum" {
				opts = append(opts, "analyze")
			}
			stmt := verb
			if len(opts) > 0 {
				stmt += " (" + strings.Join(opts, ", ") + ")"
			}
			stmt += target
			if full {
				op := safety.Operation{Level: safety.SafeWrite, Action: "vacuum full " + label, Details: []string{"the table is locked against reads and writes while it is rewritten"}, AlwaysAsk: true}
				if err := app.Allow(ctx, op); err != nil {
					return err
				}
			}
			var before, after int64
			_ = pool.QueryRow(ctx, sizeSQL, sizeArgs...).Scan(&before)
			task := app.P.Task(verb + " " + label)
			if _, err := pool.Exec(ctx, stmt); err != nil {
				task.Fail("")
				return err
			}
			_ = pool.QueryRow(ctx, sizeSQL, sizeArgs...).Scan(&after)
			detail := ui.Bytes(after)
			if verb == "vacuum" && before != after {
				detail = ui.Bytes(before) + " → " + ui.Bytes(after)
			}
			task.Done(detail)
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]any{"statement": stmt, "bytes_before": before, "bytes_after": after})
			}
			return nil
		},
	}
	if verb == "vacuum" {
		cmd.Flags().BoolVar(&full, "full", false, "rewrite the table to return space to the OS (locks it)")
		cmd.Flags().BoolVar(&withAnalyze, "analyze", true, "also refresh planner statistics")
	}
	return cmd
}

func newDBSeedCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "run the seed files",
		Long:  "Runs every file matched by seeds.paths in nexus.yaml, each in its own transaction.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			p, err := app.Project()
			if err != nil {
				return err
			}
			files, err := p.SeedFiles()
			if err != nil {
				return err
			}
			if len(files) == 0 {
				app.P.Say(ui.ToneInfo, "no seed files.", "nexus looks for "+strings.Join(p.Config.Seeds.Paths, ", ")+".")
				return nil
			}
			rel := make([]string, len(files))
			for i, f := range files {
				rel[i] = p.Rel(f)
			}
			if err := app.Allow(ctx, safety.Operation{Level: safety.SafeWrite, Action: "run " + ui.Plural(int64(len(files)), "seed file", "seed files"), Details: rel}); err != nil {
				return err
			}
			pool, err := app.DB(ctx)
			if err != nil {
				return err
			}
			task := app.P.Task("seed data")
			done, err := runSeeds(ctx, app, pool, p, files)
			if err != nil {
				task.Fail("")
				return err
			}
			task.Done(strings.Join(done, ", "))
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]any{"seeded": done})
			}
			return nil
		},
	}
}

func newDBURLCmd(app *App) *cobra.Command {
	var redacted bool
	cmd := &cobra.Command{
		Use:     "url",
		Short:   "print the connection string",
		Example: "  export DATABASE_URL=$(nexus db url)\n  nexus db url --redacted",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := app.Target(cmd.Context())
			if err != nil {
				return err
			}
			u := t.URL
			if redacted {
				u = pg.Redact(u)
			}
			if app.P.Mode().JSON {
				return app.P.JSON(map[string]string{"url": u, "environment": t.Env})
			}
			fmt.Fprintln(app.P.Out, u)
			return nil
		},
	}
	cmd.Flags().BoolVar(&redacted, "redacted", false, "mask the password")
	return cmd
}

func newQueryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "query",
		Short:   "analyze query performance",
		Example: "  nexus query analyze \"select * from users where email = 'x'\"",
		Args:    cobra.NoArgs,
	}
	cmd.AddCommand(
		newExplainCmd(app, "analyze <sql>", "run a query and explain where the time goes", true),
		newExplainCmd(app, "explain <sql>", "show the planner's estimated plan", false),
	)
	return cmd
}

package cli

import (
	"context"
	"github.com/nothikemu/nexus/internal/render"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/safety"
	"github.com/nothikemu/nexus/internal/sqltext"
	"github.com/nothikemu/nexus/internal/ui"
)

type sqlOptions struct {
	file     string
	expanded bool
	limit    int
}

func newSQLCmd(app *App) *cobra.Command {
	var o sqlOptions
	cmd := &cobra.Command{
		Use:   "sql [query]",
		Short: "run SQL, or open the SQL shell",
		Long: "With a query, a file or piped input, runs each statement and prints the results. " +
			"With nothing, opens the interactive SQL shell. Against a protected environment, " +
			"statements that write or delete data ask for confirmation first.",
		Example: "  nexus sql\n  nexus sql \"select * from users limit 5\"\n  nexus sql -f report.sql\n  echo \"select now()\" | nexus sql --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			script, source, err := sqlInput(app, o, args)
			if err != nil {
				return err
			}
			if script == "" {
				if app.P.Mode().Interactive {
					return runREPL(ctx, app)
				}
				return &ui.Problem{Title: "nothing to run.", Hint: "pass a query, use -f file.sql, or pipe SQL in", Exit: ui.ExitUsage}
			}
			return runScript(ctx, app, script, source, o)
		},
	}
	cmd.Flags().StringVarP(&o.file, "file", "f", "", "run statements from a file")
	cmd.Flags().BoolVarP(&o.expanded, "expanded", "x", false, "show each row as a record")
	cmd.Flags().IntVar(&o.limit, "limit", 1000, "most rows to keep per result (0 = all)")
	return cmd
}

func sqlInput(app *App, o sqlOptions, args []string) (script, source string, err error) {
	switch {
	case len(args) > 0:
		return strings.Join(args, " "), "", nil
	case o.file != "":
		b, err := os.ReadFile(o.file)
		if err != nil {
			return "", "", err
		}
		return string(b), o.file, nil
	case !isTerminal(os.Stdin):
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", err
		}
		return string(b), "stdin", nil
	}
	return "", "", nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runScript executes statements one at a time (each autocommits, like psql)
// and renders each result as it completes.
func runScript(ctx context.Context, app *App, script, source string, o sqlOptions) error {
	stmts := sqltext.Split(script)
	if len(stmts) == 0 {
		return &ui.Problem{Title: "nothing to run.", Detail: "the input only contains comments.", Exit: ui.ExitUsage}
	}
	t, err := app.Target(ctx)
	if err != nil {
		return err
	}
	if level, reasons := safety.ClassifySQL(script); t.Protected && level > safety.ReadOnly {
		op := safety.Operation{Level: level, Action: "run SQL that changes data", Details: reasons}
		if err := app.Allow(ctx, op); err != nil {
			return err
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

	var jsonOut []render.ResultJSON
	for _, st := range stmts {
		results, err := pg.ExecScript(ctx, conn.Conn().PgConn(), st.SQL, o.limit)
		for _, r := range results {
			if app.P.Mode().JSON {
				jsonOut = append(jsonOut, render.JSON(r))
			} else {
				printResult(app, r, o.expanded)
			}
		}
		if err != nil {
			if app.P.Mode().JSON && len(jsonOut) > 0 {
				_ = app.P.JSON(jsonOut)
			}
			return statementProblem(app, err, st, source)
		}
	}
	if app.P.Mode().JSON {
		return app.P.JSON(jsonOut)
	}
	return nil
}

func printResult(app *App, r *pg.Result, expanded bool) {
	t := app.T()
	if r.HasRows() {
		body := render.Result(t, r, expanded)
		app.P.Block(ui.Indent(body, 2) + "\n\n" + ui.Indent(render.Footer(t, r), 2))
		return
	}
	app.P.Line(t.ToneGlyph(ui.ToneSuccess) + " " + t.Text.Render(render.HumanTag(r.Command)) + "  " + t.Muted.Render(ui.Duration(r.Duration)))
}

// statementProblem renders a failed statement with a code frame pointing at
// the error, using file line numbers when the SQL came from a file.
func statementProblem(app *App, err error, st sqltext.Statement, source string) error {
	pe, ok := pg.AsPgError(err)
	if !ok {
		return err
	}
	pr := app.sqlProblem(err, st.SQL, st.Line-1, pe.Position)
	if source != "" && source != "stdin" {
		pr.Detail = strings.TrimSpace(pr.Detail + "\n" + app.T().Muted.Render(source+":"+itoa(st.Line)))
	}
	return pr
}

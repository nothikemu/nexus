package cli

import (
	"context"
	"fmt"
	"github.com/nothikemu/nexus/internal/pg/rows"
	"github.com/nothikemu/nexus/internal/render"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/ui"
)

type rowsOptions struct {
	limit    int
	offset   int
	where    string
	order    string
	search   string
	expanded bool
}

var tableViews = []string{"columns", "rows", "browse", "indexes", "relations", "policies"}

func newTableCmd(app *App) *cobra.Command {
	var o rowsOptions
	cmd := &cobra.Command{
		Use:   "table <name> [columns|rows|browse|indexes|relations|policies]",
		Short: "explore a table",
		Long: "Shows a table's structure, rows, indexes, relationships and row-level security policies. " +
			"`browse` opens the interactive explorer: search, filter, sort, page, inspect JSON, follow foreign keys and edit rows.",
		Example: "  nexus table users\n  nexus table users rows --where \"created_at > now() - interval '1 day'\" --order \"created_at desc\"\n" +
			"  nexus table users rows --search ada\n  nexus table users browse\n  nexus table users policies",
		Args: cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 1 {
				return tableViews, cobra.ShellCompDirectiveNoFileComp
			}
			return completeTables(cmd.Context(), app), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			view := "summary"
			if len(args) == 2 {
				view = args[1]
			}
			snap, tb, err := loadTable(ctx, app, args[0])
			if err != nil {
				return err
			}
			if app.P.Mode().JSON && view != "rows" {
				return app.P.JSON(tb)
			}
			t := app.T()
			switch view {
			case "summary":
				app.P.Block(render.TableHeadline(t, tb) + render.CommentLine(t, tb) + "\n\n" + render.Columns(t, tb, 2))
				var extra []string
				if len(tb.Indexes) > 0 {
					extra = append(extra, t.Section("indexes", render.Indexes(t, tb, 0)))
				}
				if len(tb.ForeignKeys)+len(tb.ReferencedBy) > 0 {
					extra = append(extra, t.Section("relations", render.Relations(t, tb, 0)))
				}
				if len(extra) > 0 {
					app.P.Block(ui.Indent(strings.Join(extra, "\n\n"), 2))
				}
				app.P.Hint("nexus table " + args[0] + " rows · browse · policies")
			case "columns":
				app.P.Block(render.TableHeadline(t, tb) + "\n\n" + render.Columns(t, tb, 2))
			case "indexes":
				app.P.Block(render.TableHeadline(t, tb) + "\n\n" + render.Indexes(t, tb, 2))
			case "relations":
				app.P.Block(render.TableHeadline(t, tb) + "\n\n" + render.Relations(t, tb, 2))
			case "policies":
				app.P.Block(render.TableHeadline(t, tb) + "\n\n" + render.Policies(t, tb, 2))
			case "rows":
				return showRows(ctx, app, tb, o)
			case "browse":
				if !app.P.Mode().Interactive {
					return &ui.Problem{Title: "browse needs an interactive terminal.", Hint: "use nexus table " + args[0] + " rows instead", Exit: ui.ExitUsage}
				}
				return runBrowser(ctx, app, snap, tb)
			default:
				return &ui.Problem{Title: fmt.Sprintf("tables don't have a %q view.", view), Detail: "try one of: " + strings.Join(tableViews, ", "), Exit: ui.ExitUsage}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVarP(&o.limit, "limit", "n", 20, "rows per page (rows view)")
	f.IntVar(&o.offset, "offset", 0, "rows to skip (rows view)")
	f.StringVarP(&o.where, "where", "w", "", "SQL filter expression (rows view)")
	f.StringVarP(&o.order, "order", "o", "", "sort, e.g. \"created_at desc\" (rows view)")
	f.StringVarP(&o.search, "search", "s", "", "case-insensitive search across all columns (rows view)")
	f.BoolVarP(&o.expanded, "expanded", "x", false, "show each row as a record (rows view)")
	return cmd
}

// loadTable resolves a table name, suggesting near matches when missing.
func loadTable(ctx context.Context, app *App, name string) (*introspect.Snapshot, *introspect.Table, error) {
	snap, err := app.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	tb := snap.Table(name)
	if tb == nil {
		pr := &ui.Problem{Title: "there's no table called " + name + ".", Code: "table_not_found", Exit: ui.ExitUsage}
		if s := snap.Suggest(name); len(s) > 0 {
			pr.Hint = "did you mean " + strings.Join(s, ", ") + "?"
		} else {
			pr.Hint = "see all tables with " + app.T().Cmd("nexus db tables")
		}
		return nil, nil, pr
	}
	return snap, tb, nil
}

func showRows(ctx context.Context, app *App, tb *introspect.Table, o rowsOptions) error {
	order, err := rows.ParseOrder(tb, o.order)
	if err != nil {
		return &ui.Problem{Title: err.Error() + ".", Exit: ui.ExitUsage}
	}
	pool, err := app.DB(ctx)
	if err != nil {
		return err
	}
	if o.limit <= 0 {
		o.limit = 20
	}
	start := time.Now()
	res, total, err := rows.Fetch(ctx, pool, rows.Query{Table: tb, Where: o.where, Search: o.search, OrderBy: order, Limit: o.limit, Offset: o.offset})
	if err != nil {
		if pe, ok := asPg(err); ok && o.where != "" {
			return app.sqlProblem(err, "", 0, pe.Position)
		}
		return err
	}
	if app.P.Mode().JSON {
		out := render.JSON(res)
		return app.P.JSON(map[string]any{"table": tb.QualifiedName(), "total": total, "offset": o.offset, "rows": out.Rows, "columns": out.Columns})
	}
	t := app.T()
	totalLabel := ui.Count(total)
	if total > rows.CountCap {
		totalLabel = "100,000+"
	}
	from, to := o.offset+1, o.offset+len(res.Rows)
	footer := fmt.Sprintf("rows %d–%d of %s", from, to, totalLabel)
	if len(res.Rows) == 0 {
		footer = "no rows match"
	}
	footer += " " + t.Glyphs.Sep + " " + ui.Duration(time.Since(start))
	head := render.TableHeadline(t, tb)
	if len(res.Rows) == 0 {
		app.P.Block(head + "\n\n  " + t.Muted.Render(footer))
		return nil
	}
	app.P.Block(head + "\n\n" + ui.Indent(render.Result(t, res, o.expanded), 2) + "\n\n  " + t.Muted.Render(footer))
	if int64(to) < total {
		app.P.Hint(fmt.Sprintf("next page: --offset %d · interactive: nexus table %s browse", to, tb.DisplayName()))
	}
	return nil
}

// completeTables lists table names for shell completion.
func completeTables(ctx context.Context, app *App) []string {
	if app.P == nil {
		app.P = ui.NewPrinter(ui.PlainMode())
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	snap, err := app.Snapshot(c)
	if err != nil {
		return nil
	}
	var names []string
	for _, tb := range snap.Tables {
		names = append(names, tb.DisplayName())
	}
	return names
}

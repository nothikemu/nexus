package repl

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/explain"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/render"
	"github.com/nothikemu/nexus/internal/ui"
)

// meta runs a backslash command.
func (m *model) meta(text string) tea.Cmd {
	fields := strings.Fields(text)
	cmd, arg := fields[0], strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	t := m.t
	say := func(s string) tea.Cmd { return func() tea.Msg { return metaMsg{out: s} } }
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)

	switch cmd {
	case `\q`, `\quit`, `\exit`:
		cancel()
		return func() tea.Msg { return metaMsg{quit: true, out: t.Muted.Render("  " + ui.Say(ui.MomentGoodbye))} }
	case `\?`, `\h`, `\help`:
		cancel()
		return say(help(t))
	case `\x`:
		cancel()
		m.expanded = !m.expanded
		return say("  " + t.Muted.Render("expanded display is "+onOff(m.expanded)+"."))
	case `\timing`:
		cancel()
		m.timing = !m.timing
		return say("  " + t.Muted.Render("timing is "+onOff(m.timing)+"."))
	case `\history`:
		cancel()
		var lines []string
		for _, h := range m.history.last(20) {
			lines = append(lines, "  "+t.Text.Render(strings.ReplaceAll(h, "\n", " ")))
		}
		return say(strings.Join(lines, "\n"))
	}

	conn := m.conn
	schemas := m.o.Schemas
	return func() tea.Msg {
		defer cancel()
		out, err := func() (string, error) {
			switch cmd {
			case `\conninfo`:
				cfg := conn.Conn().Config()
				v, _, err := pg.ServerVersion(ctx, conn)
				if err != nil {
					return "", err
				}
				return "  " + t.Text.Render(fmt.Sprintf("connected to %s as %s on %s:%d", cfg.Database, cfg.User, cfg.Host, cfg.Port)) +
					t.Muted.Render(" · PostgreSQL "+v+" · "+m.o.Env), nil
			case `\dn`:
				all, err := introspect.Schemas(ctx, conn)
				if err != nil {
					return "", err
				}
				return "  " + t.Text.Render(strings.Join(all, "  ")), nil
			case `\dt`, `\d`:
				snap, err := introspect.Load(ctx, conn, schemas)
				if err != nil {
					return "", err
				}
				if cmd == `\dt` || arg == "" {
					var tables []*introspect.Table
					for _, tb := range snap.Tables {
						if !tb.IsView() || cmd == `\d` {
							tables = append(tables, tb)
						}
					}
					if len(tables) == 0 {
						return "  " + t.Muted.Render("no tables."), nil
					}
					return render.Tables(t, tables), nil
				}
				tb := snap.Table(arg)
				if tb == nil {
					msg := "no table called " + arg + "."
					if s := snap.Suggest(arg); len(s) > 0 {
						msg += " did you mean " + strings.Join(s, ", ") + "?"
					}
					return "  " + t.Warning.Render(msg), nil
				}
				out := "  " + render.TableHeadline(t, tb) + "\n\n" + render.Columns(t, tb, 2)
				if len(tb.Indexes) > 0 {
					out += "\n\n" + render.Indexes(t, tb, 2)
				}
				return out, nil
			case `\explain`, `\analyze`:
				if arg == "" {
					return "  " + t.Muted.Render("usage: "+cmd+" <query>"), nil
				}
				plan, err := explain.Run(ctx, conn.Conn(), arg, cmd == `\analyze`)
				if err != nil {
					return "", err
				}
				snap, _ := introspect.Load(ctx, conn, schemas)
				out := render.Plan(t, plan) + "\n\n  " + render.PlanSummary(t, plan)
				if ins := explain.Analyze(plan, snap); len(ins) > 0 {
					out += "\n\n" + ui.Indent(render.Insights(t, ins), 2)
				}
				return out, nil
			}
			return "  " + t.Warning.Render("unknown command "+cmd+".") + t.Muted.Render(` \? lists them.`), nil
		}()
		if err != nil {
			pr := render.SQLProblem(t, err, "", 0, 0)
			return metaMsg{out: ui.Indent(t.ProblemString(pr), 2)}
		}
		return metaMsg{out: out}
	}
}

func help(t *ui.Theme) string {
	rows := [][2]string{
		{`\dt`, "list tables"},
		{`\d [name]`, "describe a table (or list everything)"},
		{`\dn`, "list schemas"},
		{`\explain <q>`, "show the plan for a query"},
		{`\analyze <q>`, "run a query and explain it (rolled back)"},
		{`\x`, "toggle expanded records"},
		{`\timing`, "toggle timing"},
		{`\conninfo`, "connection details"},
		{`\history`, "recent commands"},
		{`\q`, "quit"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, "  "+t.Secondary.Render(ui.PadRight(r[0], 14))+t.Text.Render(r[1]))
	}
	lines = append(lines, "",
		"  "+t.Muted.Render("enter runs a statement once it ends with ; · alt+enter adds a line"),
		"  "+t.Muted.Render("↑↓ history · ctrl+c cancels a query or clears the line · ctrl+d quits"))
	return strings.Join(lines, "\n")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

package render

import (
	"fmt"
	"strings"

	"github.com/nothikemu/nexus/internal/pg/explain"
	"github.com/nothikemu/nexus/internal/ui"
)

// Plan draws a plan as an annotated tree. Each node shows a bar of
// its share of execution time (or of estimated cost without --analyze).
func Plan(t *ui.Theme, p *explain.Plan) string {
	type line struct {
		label, bar, metric, rows string
		notes                    []string
		depth                    int
	}
	var lines []line
	total := p.ExecutionMs
	if !p.Analyzed || total <= 0 {
		total = p.Root.TotalCost
	}
	p.Walk(func(n *explain.Node, depth int) {
		l := line{label: n.Label(), depth: depth}
		var share float64
		if p.Analyzed && p.ExecutionMs > 0 {
			share = n.SelfMs() / total
			l.metric = ui.Millis(n.SelfMs())
			l.rows = rowsLabel(t, n, true)
		} else {
			self := n.TotalCost
			for _, c := range n.Children {
				self -= c.TotalCost
			}
			if self < 0 {
				self = 0
			}
			if total > 0 {
				share = self / total
			}
			l.metric = fmt.Sprintf("cost %.0f", n.TotalCost)
			l.rows = rowsLabel(t, n, false)
		}
		hex := t.Palette.Primary
		if n.Type == "Seq Scan" {
			hex = t.Palette.Warning
		} else if strings.Contains(n.Type, "Index") {
			hex = t.Palette.Success
		}
		l.bar = t.Bar(share, 10, hex)
		add := func(k, v string) {
			if v != "" {
				l.notes = append(l.notes, t.Muted.Render(k+" ")+t.Text.Render(v))
			}
		}
		add("index cond", n.IndexCond)
		add("filter", n.Filter)
		add("join filter", n.JoinFilter)
		add("hash cond", n.HashCond)
		add("merge cond", n.MergeCond)
		add("recheck", n.RecheckCond)
		if len(n.SortKey) > 0 {
			add("sort key", strings.Join(n.SortKey, ", "))
		}
		if n.SortMethod != "" {
			add("sort", fmt.Sprintf("%s · %.0f kB %s", n.SortMethod, n.SortSpaceUsed, strings.ToLower(n.SortSpaceType)))
		}
		if p.Analyzed && n.RemovedFilter > 0 {
			l.notes = append(l.notes, t.Warning.Render(ui.Count(int64(n.RemovedFilter*loopsOf(n)))+" rows removed by filter"))
		}
		lines = append(lines, l)
	})

	labelW := 0
	for _, l := range lines {
		if w := l.depth*3 + ui.Width(l.label); w > labelW {
			labelW = w
		}
	}
	if max := t.Mode.ContentWidth() - 46; labelW > max {
		labelW = max
	}
	var b strings.Builder
	for i, l := range lines {
		prefix := strings.Repeat("   ", l.depth)
		branch := ""
		if l.depth > 0 {
			prefix = strings.Repeat("   ", l.depth-1)
			branch = t.Faint.Render("└─ ")
		}
		label := t.Strong.Render(l.label)
		b.WriteString("  " + ui.PadRight(prefix+branch+label, labelW+3) + l.bar + "  " + t.Text.Render(ui.PadLeft(l.metric, 9)) + "  " + l.rows + "\n")
		for _, note := range l.notes {
			b.WriteString("  " + strings.Repeat("   ", l.depth) + "   " + note + "\n")
		}
		if i < len(lines)-1 && len(l.notes) > 0 {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func loopsOf(n *explain.Node) float64 {
	if n.Loops <= 0 {
		return 1
	}
	return n.Loops
}

func rowsLabel(t *ui.Theme, n *explain.Node, analyzed bool) string {
	if analyzed {
		if n.IsScan() && n.RemovedFilter > 0 {
			read := n.RowsRead(true)
			return t.Muted.Render(ui.Count(int64(read))+" → ") + t.Number.Render(ui.Count(int64(n.RowsOut(true)))) + t.Muted.Render(" rows")
		}
		return t.Number.Render(ui.Count(int64(n.RowsOut(true)))) + t.Muted.Render(" rows")
	}
	return t.Muted.Render("~") + t.Number.Render(ui.Count(int64(n.PlanRows))) + t.Muted.Render(" rows")
}

// PlanSummary is the line under the plan.
func PlanSummary(t *ui.Theme, p *explain.Plan) string {
	tot := p.Totals()
	parts := []string{
		"scanned " + ui.Count(int64(tot.RowsScanned)) + " rows",
		"returned " + ui.Count(int64(tot.RowsReturned)),
		fmt.Sprintf("cost %.0f", tot.TotalCost),
	}
	if p.Analyzed && tot.SharedHit+tot.SharedRead > 0 {
		parts = append(parts, "cache hit "+ui.Percent(tot.SharedHit/(tot.SharedHit+tot.SharedRead)))
	}
	return t.Muted.Render(strings.Join(parts, " "+t.Glyphs.Sep+" "))
}

// Insights lists findings with their suggested SQL.
func Insights(t *ui.Theme, ins []explain.Insight) string {
	var lines []string
	for i, in := range ins {
		if i > 0 {
			lines = append(lines, "")
		}
		var glyph string
		switch in.Severity {
		case explain.Warning:
			glyph = t.ToneGlyph(ui.ToneWarning)
		case explain.Good:
			glyph = t.ToneGlyph(ui.ToneSuccess)
		default:
			glyph = t.ToneGlyph(ui.ToneInfo)
		}
		lines = append(lines, glyph+" "+t.Strong.Render(in.Title))
		if in.Detail != "" {
			lines = append(lines, ui.Indent(t.Muted.Render(ui.Wrap(in.Detail, t.Mode.ContentWidth()-8)), 2))
		}
		if in.SQL != "" {
			lines = append(lines, "", "    "+t.Code.Render(in.SQL))
		}
	}
	return strings.Join(lines, "\n")
}

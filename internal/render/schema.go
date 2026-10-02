package render

import (
	"fmt"
	"strings"

	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/stats"
	"github.com/nothikemu/nexus/internal/ui"
)

// TableHeadline is "users  table · ~3 rows · 32 KB".
func TableHeadline(t *ui.Theme, tb *introspect.Table) string {
	meta := []string{tb.Kind}
	if !tb.IsView() || tb.Kind == introspect.KindMatView {
		meta = append(meta, "~"+ui.Plural(tb.Rows, "row", "rows"), ui.Bytes(tb.Bytes))
	}
	if tb.RLSEnabled {
		meta = append(meta, "row level security")
	}
	if tb.PartitionOf != "" {
		meta = append(meta, "partition of "+tb.PartitionOf)
	}
	return t.ToneGlyph(ui.ToneNexus) + " " + t.Title.Render(tb.DisplayName()) + "   " + t.Muted.Render(strings.Join(meta, " "+t.Glyphs.Sep+" "))
}

// Columns renders a table's columns with their attributes.
func Columns(t *ui.Theme, tb *introspect.Table, indent int) string {
	out := ui.Table{Indent: indent, Columns: []ui.Column{{Title: "column"}, {Title: "type"}, {Title: "attributes", Flex: true}, {Title: "default", Flex: true}}}
	for _, c := range tb.Columns {
		var attrs []string
		name := t.Strong.Render(c.Name)
		if tb.IsPrimaryKey(c.Name) {
			attrs = append(attrs, t.Spark.Render(t.Glyphs.Key+" primary key"))
		} else {
			if tb.IsUnique(c.Name) {
				attrs = append(attrs, t.Secondary.Render("unique"))
			}
			if !c.Nullable {
				attrs = append(attrs, t.Text.Render("not null"))
			}
		}
		if fk := tb.ForeignKeyFor(c.Name); fk != nil {
			attrs = append(attrs, t.Info.Render(t.Glyphs.Link+" "+RefName(tb, fk)+"."+fk.RefColumns[0]))
		}
		if c.Identity != "" {
			attrs = append(attrs, t.Muted.Render("identity "+c.Identity))
		}
		def := c.Default
		if c.Generated != "" {
			attrs = append(attrs, t.Muted.Render("generated"))
			def = c.Generated
		}
		out.Rows = append(out.Rows, []string{name, t.Secondary.Render(c.Type), strings.Join(attrs, t.Faint.Render(" · ")), t.Muted.Render(def)})
	}
	return t.Table(out)
}

func RefName(from *introspect.Table, fk *introspect.ForeignKey) string {
	if fk.RefSchema == from.Schema || fk.RefSchema == "public" {
		return fk.RefTable
	}
	return fk.RefSchema + "." + fk.RefTable
}

func Indexes(t *ui.Theme, tb *introspect.Table, indent int) string {
	if len(tb.Indexes) == 0 {
		return ui.Indent(t.Muted.Render("no indexes"), indent)
	}
	out := ui.Table{Indent: indent, Columns: []ui.Column{{Title: "index"}, {Title: "method"}, {Title: "columns", Flex: true}, {Title: "", Flex: true}, {Title: "size", Align: ui.AlignRight}, {Title: "scans", Align: ui.AlignRight}}}
	for _, ix := range tb.Indexes {
		var flags []string
		if ix.Primary {
			flags = append(flags, t.Spark.Render("primary"))
		} else if ix.Unique {
			flags = append(flags, t.Secondary.Render("unique"))
		}
		if ix.Predicate != "" {
			flags = append(flags, t.Muted.Render("where "+ix.Predicate))
		}
		if !ix.Valid {
			flags = append(flags, t.Error.Render("invalid"))
		}
		out.Rows = append(out.Rows, []string{
			t.Strong.Render(ix.Name), t.Muted.Render(ix.Method), t.Text.Render(strings.Join(ix.Columns, ", ")),
			strings.Join(flags, t.Faint.Render(" · ")), t.Muted.Render(ui.Bytes(ix.Bytes)), t.Number.Render(ui.Count(ix.Scans)),
		})
	}
	return t.Table(out)
}

func Relations(t *ui.Theme, tb *introspect.Table, indent int) string {
	var lines []string
	for _, fk := range tb.ForeignKeys {
		line := t.Info.Render(t.Glyphs.Link) + " " + t.Strong.Render(strings.Join(fk.Columns, ", ")) +
			t.Muted.Render(" → ") + t.Text.Render(RefName(tb, fk)+"("+strings.Join(fk.RefColumns, ", ")+")")
		if fk.OnDelete != "no action" {
			line += t.Muted.Render("  on delete " + fk.OnDelete)
		}
		lines = append(lines, line)
	}
	for _, fk := range tb.ReferencedBy {
		from := fk.Table
		if fk.Schema != tb.Schema && fk.Schema != "public" {
			from = fk.Qualified()
		}
		lines = append(lines, t.Secondary.Render("↙")+" "+t.Text.Render(from+"("+strings.Join(fk.Columns, ", ")+")")+
			t.Muted.Render(" → ")+t.Strong.Render(strings.Join(fk.RefColumns, ", ")))
	}
	if len(lines) == 0 {
		return ui.Indent(t.Muted.Render("no relationships"), indent)
	}
	return ui.Indent(strings.Join(lines, "\n"), indent)
}

func Policies(t *ui.Theme, tb *introspect.Table, indent int) string {
	var lines []string
	state := t.Warning.Render("disabled")
	if tb.RLSEnabled {
		state = t.Success.Render("enabled")
		if tb.RLSForced {
			state += t.Muted.Render(" · forced for the owner too")
		}
	}
	lines = append(lines, t.Key.Render("row level security  ")+state)
	if len(tb.Policies) == 0 {
		msg := "no policies"
		if tb.RLSEnabled {
			msg += " — with RLS enabled and no policies, only the owner and superusers can see rows"
		}
		lines = append(lines, t.Muted.Render(msg))
	}
	for _, p := range tb.Policies {
		kind := "permissive"
		if !p.Permissive {
			kind = "restrictive"
		}
		lines = append(lines, "", t.Strong.Render(p.Name)+"  "+t.Secondary.Render(p.Command)+t.Muted.Render(" · "+kind+" · to "+strings.Join(p.Roles, ", ")))
		if p.Using != "" {
			lines = append(lines, t.Muted.Render("  using  ")+t.Text.Render(p.Using))
		}
		if p.Check != "" {
			lines = append(lines, t.Muted.Render("  check  ")+t.Text.Render(p.Check))
		}
	}
	return ui.Indent(strings.Join(lines, "\n"), indent)
}

// Tables lists relations: name, kind, rows, size, columns.
func Tables(t *ui.Theme, tables []*introspect.Table) string {
	out := ui.Table{Indent: 2, Columns: []ui.Column{
		{Title: "name"}, {Title: "kind"}, {Title: "rows", Align: ui.AlignRight}, {Title: "size", Align: ui.AlignRight},
		{Title: "columns", Align: ui.AlignRight}, {Title: "", Flex: true},
	}}
	for _, tb := range tables {
		var notes []string
		if len(tb.PrimaryKey) == 0 && !tb.IsView() {
			notes = append(notes, t.Warning.Render("no primary key"))
		}
		if tb.RLSEnabled {
			notes = append(notes, t.Muted.Render(t.Glyphs.Lock+" rls"))
		}
		if tb.Comment != "" {
			notes = append(notes, t.Muted.Render(tb.Comment))
		}
		rows, size := t.Number.Render(ui.Count(tb.Rows)), t.Muted.Render(ui.Bytes(tb.Bytes))
		if tb.Kind == introspect.KindView {
			rows, size = t.Faint.Render("–"), t.Faint.Render("–")
		}
		out.Rows = append(out.Rows, []string{
			t.Strong.Render(tb.DisplayName()), t.Muted.Render(tb.Kind), rows, size,
			t.Text.Render(fmt.Sprint(len(tb.Columns))), strings.Join(notes, t.Faint.Render(" · ")),
		})
	}
	return t.Table(out)
}

// Findings renders health findings.
func Findings(t *ui.Theme, findings []stats.Finding, indent int, withFix bool) string {
	var lines []string
	for i, f := range findings {
		if i > 0 {
			lines = append(lines, "")
		}
		tone := ui.ToneWarning
		if f.Severity == stats.SevInfo {
			tone = ui.ToneInfo
		}
		lines = append(lines, t.ToneGlyph(tone)+" "+t.Strong.Render(f.Title))
		lines = append(lines, ui.Indent(t.Muted.Render(ui.Wrap(f.Detail, t.Mode.ContentWidth()-indent-4)), 2))
		objs := f.Objects
		if len(objs) > 6 {
			objs = append(objs[:6:6], fmt.Sprintf("and %d more", len(f.Objects)-6))
		}
		lines = append(lines, "  "+t.Text.Render(strings.Join(objs, ", ")))
		if withFix {
			for j, fix := range f.Fix {
				if j == 3 {
					lines = append(lines, "  "+t.Muted.Render(fmt.Sprintf("… %d more", len(f.Fix)-3)))
					break
				}
				lines = append(lines, "  "+t.Code.Render(fix))
			}
		}
	}
	return ui.Indent(strings.Join(lines, "\n"), indent)
}

func CommentLine(t *ui.Theme, tb *introspect.Table) string {
	if tb.Comment == "" {
		return ""
	}
	return "\n  " + t.Muted.Render(tb.Comment)
}

func SchemaTree(t *ui.Theme, snap *introspect.Snapshot, withColumns bool) string {
	var out []string
	for _, schema := range snap.Schemas {
		var groups []ui.TreeNode
		var tables, views []ui.TreeNode
		for _, tb := range snap.Tables {
			if tb.Schema != schema {
				continue
			}
			label := t.Strong.Render(tb.Name) + "  " + t.Muted.Render(ui.Plural(int64(len(tb.Columns)), "column", "columns"))
			if !tb.IsView() {
				label += t.Muted.Render(" · ~" + ui.Plural(tb.Rows, "row", "rows"))
			}
			if tb.PartitionOf != "" {
				label += t.Muted.Render(" · partition of " + tb.PartitionOf)
			}
			node := ui.TreeNode{Label: label}
			if withColumns {
				for _, c := range tb.Columns {
					l := t.Text.Render(c.Name) + "  " + t.Secondary.Render(c.Type)
					if tb.IsPrimaryKey(c.Name) {
						l += " " + t.Spark.Render(t.Glyphs.Key)
					}
					if fk := tb.ForeignKeyFor(c.Name); fk != nil {
						l += " " + t.Info.Render(t.Glyphs.Link+" "+RefName(tb, fk))
					}
					node.Children = append(node.Children, ui.TreeNode{Label: l})
				}
			}
			if tb.IsView() {
				views = append(views, node)
			} else {
				tables = append(tables, node)
			}
		}
		var funcs, enums, seqs []ui.TreeNode
		for _, f := range snap.Functions {
			if f.Schema == schema {
				l := t.Strong.Render(f.Name) + t.Muted.Render("("+f.Arguments+")")
				if f.Returns != "" {
					l += t.Muted.Render(" → ") + t.Secondary.Render(f.Returns)
				}
				funcs = append(funcs, ui.TreeNode{Label: l + "  " + t.Faint.Render(f.Language)})
			}
		}
		for _, e := range snap.Enums {
			if e.Schema == schema {
				enums = append(enums, ui.TreeNode{Label: t.Strong.Render(e.Name) + "  " + t.Muted.Render(strings.Join(e.Values, ", "))})
			}
		}
		for _, s := range snap.Sequences {
			if s.Schema == schema {
				l := t.Strong.Render(s.Name)
				if s.OwnedBy != "" {
					l += "  " + t.Muted.Render("owned by "+s.OwnedBy)
				}
				seqs = append(seqs, ui.TreeNode{Label: l})
			}
		}
		add := func(name string, nodes []ui.TreeNode) {
			if len(nodes) > 0 {
				groups = append(groups, ui.TreeNode{Label: t.Heading.Render(name), Children: nodes})
			}
		}
		add("tables", tables)
		add("views", views)
		add("functions", funcs)
		add("enums", enums)
		add("sequences", seqs)
		head := t.Primary.Bold(true).Render(schema)
		if len(groups) == 0 {
			out = append(out, head+"  "+t.Muted.Render("empty"))
			continue
		}
		out = append(out, head+"\n"+t.Tree(groups))
	}
	return strings.Join(out, "\n\n")
}

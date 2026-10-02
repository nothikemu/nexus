package render

import (
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/migrate"
	"github.com/nothikemu/nexus/internal/pg/schemadiff"
	"github.com/nothikemu/nexus/internal/ui"
)

// MigrationStatus lists migrations with their state.
func MigrationStatus(t *ui.Theme, st *migrate.Status) string {
	if len(st.Entries) == 0 {
		return ui.Indent(t.Muted.Render("no migrations yet — create one with ")+t.Cmd("nexus migration create <name>"), 2)
	}
	now := time.Now()
	out := ui.Table{Indent: 2, NoHeader: true, Columns: []ui.Column{{Title: ""}, {Title: "version"}, {Title: "name", Flex: true}, {Title: "state", Flex: true}}}
	for _, e := range st.Entries {
		var glyph, state string
		switch e.State {
		case migrate.StateApplied:
			glyph = t.Success.Render(t.Glyphs.Check)
			state = t.Muted.Render("applied " + ui.Ago(*e.AppliedAt, now) + " " + t.Glyphs.Sep + " " + ui.Duration(time.Duration(e.DurationMs)*time.Millisecond))
		case migrate.StatePending:
			glyph = t.Primary.Render(t.Glyphs.Pending)
			state = t.Primary.Render("pending")
			if e.OutOfOrder {
				state += t.Warning.Render(" · out of order")
			}
			if e.Empty {
				state += t.Warning.Render(" · empty")
			}
		case migrate.StateModified:
			glyph = t.Warning.Render(t.Glyphs.Tilde)
			state = t.Warning.Render("edited after it was applied")
		case migrate.StateMissing:
			glyph = t.Error.Render(t.Glyphs.Minus)
			state = t.Error.Render("applied, but the file is gone")
		}
		if !e.Reversible && e.State != migrate.StateMissing {
			state += t.Faint.Render(" · no down")
		}
		out.Rows = append(out.Rows, []string{glyph, t.Muted.Render(e.Version), t.Strong.Render(e.Name), state})
	}
	return t.Table(out)
}

// MigrationSummary is a one-line summary: "up to date · 3 applied".
func MigrationSummary(t *ui.Theme, st *migrate.Status) (string, ui.Tone) {
	switch {
	case st.Modified > 0 || st.Missing > 0:
		return ui.Plural(int64(st.Modified+st.Missing), "migration has", "migrations have") + " drifted", ui.ToneWarning
	case st.Pending > 0:
		return ui.Plural(int64(st.Pending), "pending migration", "pending migrations"), ui.ToneInfo
	case st.Applied == 0:
		return "no migrations applied", ui.ToneInfo
	default:
		return "up to date " + t.Glyphs.Sep + " " + ui.Plural(int64(st.Applied), "applied", "applied"), ui.ToneSuccess
	}
}

// Diff draws a schema diff grouped by table.
func Diff(t *ui.Theme, d schemadiff.Diff) string {
	if d.Empty() {
		return ui.Indent(t.Muted.Render("no schema changes"), 2)
	}
	sym := func(k schemadiff.Kind) string {
		switch k {
		case schemadiff.Added:
			return t.Success.Render(t.Glyphs.Plus)
		case schemadiff.Removed:
			return t.Error.Render(t.Glyphs.Minus)
		default:
			return t.Warning.Render(t.Glyphs.Tilde)
		}
	}
	var lines []string
	parentShown := map[string]bool{}
	for _, c := range d.Changes {
		if c.Parent == "" {
			parentShown[c.Name] = true
			line := "  " + sym(c.Kind) + " " + t.Muted.Render(c.Object+" ") + t.Strong.Render(c.Name)
			if c.Detail != "" {
				line += "  " + t.Muted.Render(c.Detail)
			}
			lines = append(lines, line)
			continue
		}
		if !parentShown[c.Parent] {
			parentShown[c.Parent] = true
			lines = append(lines, "  "+t.Warning.Render(t.Glyphs.Tilde)+" "+t.Muted.Render("table ")+t.Strong.Render(c.Parent))
		}
		line := "      " + sym(c.Kind) + " " + t.Muted.Render(c.Object+" ") + t.Text.Render(c.Name)
		if c.Detail != "" && c.Object != "index" && c.Object != "constraint" && c.Object != "foreign key" && c.Object != "trigger" && c.Object != "policy" {
			line += "  " + t.Muted.Render(c.Detail)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// DiffSummary is "3 tables changed · 5 additions".
func DiffSummary(t *ui.Theme, d schemadiff.Diff) string {
	var parts []string
	parts = append(parts, ui.Plural(int64(d.TablesTouched()), "table changed", "tables changed"))
	if n := d.Count(schemadiff.Added); n > 0 {
		parts = append(parts, ui.Plural(int64(n), "addition", "additions"))
	}
	if n := d.Count(schemadiff.Changed); n > 0 {
		parts = append(parts, ui.Plural(int64(n), "change", "changes"))
	}
	if n := d.Count(schemadiff.Removed); n > 0 {
		parts = append(parts, ui.Plural(int64(n), "removal", "removals"))
	}
	return strings.Join(parts, " "+t.Glyphs.Sep+" ")
}

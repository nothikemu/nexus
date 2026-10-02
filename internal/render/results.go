package render

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/ui"
)

// PostgreSQL type OIDs Nexus formats specially.
const (
	oidBool    = 16
	oidInt8    = 20
	oidInt2    = 21
	oidInt4    = 23
	oidOID     = 26
	oidJSON    = 114
	oidFloat4  = 700
	oidFloat8  = 701
	oidMoney   = 790
	oidNumeric = 1700
	oidJSONB   = 3802
)

func IsNumeric(oid uint32) bool {
	switch oid {
	case oidInt2, oidInt4, oidInt8, oidOID, oidFloat4, oidFloat8, oidNumeric, oidMoney:
		return true
	}
	return false
}

// Cell renders one value for the terminal.
func Cell(t *ui.Theme, c pg.Cell, oid uint32) string {
	if c.Null {
		return t.Null.Render("null")
	}
	switch {
	case oid == oidBool:
		if c.Text == "t" {
			return t.Success.Render("true")
		}
		return t.Muted.Render("false")
	case IsNumeric(oid):
		return t.Number.Render(c.Text)
	case oid == oidJSON || oid == oidJSONB:
		return t.Text.Render(compactJSON(c.Text))
	}
	return t.Text.Render(strings.ReplaceAll(c.Text, "\n", "↵"))
}

func compactJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}

// HumanTag turns a command tag into words: "INSERT 0 3" → "inserted 3 rows".
func HumanTag(tag string) string {
	f := strings.Fields(tag)
	if len(f) == 0 {
		return ""
	}
	n, err := strconv.ParseInt(f[len(f)-1], 10, 64)
	verbs := map[string]string{"INSERT": "inserted", "UPDATE": "updated", "DELETE": "deleted", "MERGE": "merged", "COPY": "copied", "SELECT": "selected"}
	if v, ok := verbs[f[0]]; ok && err == nil {
		return v + " " + ui.Plural(n, "row", "rows")
	}
	return strings.ToLower(tag)
}

// Result draws a result set as a table, or expanded (one record per
// block) when expanded is set or the table is far too wide.
func Result(t *ui.Theme, r *pg.Result, expanded bool) string {
	if !r.HasRows() {
		return ""
	}
	if len(r.Rows) == 0 {
		cols := make([]string, len(r.Columns))
		for i, c := range r.Columns {
			cols[i] = c.Name
		}
		return t.Muted.Render("(no rows)  " + strings.Join(cols, ", "))
	}
	if expanded {
		return records(t, r)
	}
	tb := ui.Table{}
	for _, c := range r.Columns {
		col := ui.Column{Title: c.Name, Min: 3}
		if IsNumeric(c.OID) {
			col.Align = ui.AlignRight
		}
		tb.Columns = append(tb.Columns, col)
	}
	for _, row := range r.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = Cell(t, c, r.Columns[i].OID)
		}
		tb.Rows = append(tb.Rows, cells)
	}
	return t.Table(tb)
}

func records(t *ui.Theme, r *pg.Result) string {
	kw := 0
	for _, c := range r.Columns {
		if w := ui.Width(c.Name); w > kw {
			kw = w
		}
	}
	var blocks []string
	for i, row := range r.Rows {
		var b strings.Builder
		b.WriteString(t.Faint.Render(fmt.Sprintf("── record %d ", i+1)) + t.Faint.Render(strings.Repeat(t.Glyphs.Rule, 20)) + "\n")
		for j, c := range row {
			b.WriteString(t.Key.Render(ui.PadRight(r.Columns[j].Name, kw)) + "   " + Cell(t, c, r.Columns[j].OID))
			if j < len(row)-1 {
				b.WriteString("\n")
			}
		}
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n")
}

// Footer is the line under a result: "3 rows · 1.2ms".
func Footer(t *ui.Theme, r *pg.Result) string {
	var parts []string
	if r.HasRows() {
		parts = append(parts, ui.Plural(r.Total, "row", "rows"))
		if r.Truncated {
			parts = append(parts, fmt.Sprintf("showing first %d", len(r.Rows)))
		}
	} else if r.Command != "" {
		parts = append(parts, HumanTag(r.Command))
	}
	parts = append(parts, ui.Duration(r.Duration))
	return t.Muted.Render(strings.Join(parts, " "+t.Glyphs.Sep+" "))
}

// ResultJSON is the --json shape of a statement result.
type ResultJSON struct {
	Command    string           `json:"command"`
	Columns    []pg.Column      `json:"columns,omitempty"`
	Rows       []map[string]any `json:"rows,omitempty"`
	RowCount   int64            `json:"row_count"`
	Truncated  bool             `json:"truncated,omitempty"`
	DurationMs float64          `json:"duration_ms"`
}

// JSON converts text cells into typed JSON values.
func JSON(r *pg.Result) ResultJSON {
	out := ResultJSON{Command: r.Command, Columns: r.Columns, RowCount: r.Total, Truncated: r.Truncated, DurationMs: float64(r.Duration) / float64(time.Millisecond)}
	if !r.HasRows() {
		out.RowCount = r.Affected
	}
	for _, row := range r.Rows {
		m := make(map[string]any, len(row))
		for i, c := range row {
			m[r.Columns[i].Name] = jsonValue(c, r.Columns[i].OID)
		}
		out.Rows = append(out.Rows, m)
	}
	return out
}

func jsonValue(c pg.Cell, oid uint32) any {
	if c.Null {
		return nil
	}
	switch {
	case oid == oidBool:
		return c.Text == "t"
	case oid == oidJSON || oid == oidJSONB:
		if json.Valid([]byte(c.Text)) {
			return json.RawMessage(c.Text)
		}
	case IsNumeric(oid) && oid != oidMoney:
		if _, err := strconv.ParseFloat(c.Text, 64); err == nil {
			return json.Number(c.Text)
		}
	}
	return c.Text
}

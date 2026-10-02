package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/rows"
	"github.com/nothikemu/nexus/internal/render"
	"github.com/nothikemu/nexus/internal/tui/tuikit"
	"github.com/nothikemu/nexus/internal/ui"
	"github.com/nothikemu/nexus/internal/ui/mascot"
)

const maxColWidth = 36

// View implements tea.Model.
func (m *Model) View() string {
	if m.mode == modeDetail {
		return m.detailView()
	}
	t := m.t
	w := m.width
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	b.WriteString(t.Rule(w) + "\n")
	b.WriteString(tuikit.FitLines(m.grid(), m.pageSize()+1) + "\n")
	b.WriteString(t.Rule(w) + "\n")
	b.WriteString(ui.Truncate(m.status(), w, "…") + "\n")
	b.WriteString(ui.Truncate(" "+m.hints(), w, "…"))
	return b.String()
}

func (m *Model) header() string {
	t := m.t
	v := m.top()
	state := mascot.Idle
	if m.loading {
		state = mascot.Working
	} else if m.loadErr != nil {
		state = mascot.Error
	}
	face := mascot.Face(t, state, m.frame)
	if face == "" {
		face = mascot.Glyph(t, state)
	}
	title := t.Title.Render(v.table.DisplayName())
	meta := t.Muted.Render(v.table.Kind)
	if v.via != "" {
		meta += t.Faint.Render("  ← " + v.via)
	}
	left := " " + face + "  " + title + "  " + meta
	var filters []string
	if v.search != "" {
		filters = append(filters, t.Muted.Render("search ")+t.Secondary.Render(v.search))
	}
	if v.where != "" {
		filters = append(filters, t.Muted.Render("where ")+t.Secondary.Render(ui.Truncate(v.where, 40, "…")))
	}
	for _, o := range v.order {
		dir := t.Glyphs.Up
		if o.Desc {
			dir = t.Glyphs.Down
		}
		filters = append(filters, t.Muted.Render("sort ")+t.Secondary.Render(o.Column+" "+dir))
	}
	right := strings.Join(filters, t.Faint.Render("  ·  ")) + " "
	gap := m.width - ui.Width(left) - ui.Width(right)
	if gap < 2 {
		return ui.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// layout decides which columns are visible and how wide they are.
func (m *Model) layout() (first, last int, widths []int) {
	v := m.top()
	cols := m.res.Columns
	widths = make([]int, len(cols))
	for i, c := range cols {
		w := ui.Width(c.Name) + 2 // room for markers
		for _, r := range m.res.Rows {
			if cw := ui.Width(displayText(r[i], c.OID)); cw > w {
				w = cw
			}
		}
		widths[i] = min(w, maxColWidth)
	}
	avail := m.width - 3
	if v.col < v.colOffset {
		v.colOffset = v.col
	}
	fits := func(from, to int) bool {
		total := 0
		for i := from; i <= to; i++ {
			total += widths[i] + 2
		}
		return total <= avail
	}
	for v.colOffset < v.col && !fits(v.colOffset, v.col) {
		v.colOffset++
	}
	first, last = v.colOffset, v.colOffset
	for last+1 < len(cols) && fits(first, last+1) {
		last++
	}
	return first, last, widths
}

func (m *Model) grid() string {
	t := m.t
	if m.loadErr != nil {
		return "\n " + t.ToneGlyph(ui.ToneError) + " " + t.Error.Render(errText(m.loadErr)) + "\n\n " +
			t.Muted.Render("press f to change the filter, esc to clear it")
	}
	if m.res == nil {
		return "\n " + tuikit.Spark(t, m.frame) + " " + t.Shimmer("nexus is reading "+m.top().table.DisplayName()+"…", m.frame)
	}
	if len(m.res.Rows) == 0 {
		msg := "no rows yet."
		if m.top().where != "" || m.top().search != "" {
			msg = "no rows match."
		}
		return "\n " + t.Muted.Render(msg)
	}
	v := m.top()
	first, last, widths := m.layout()
	tb := v.table
	var b strings.Builder

	// Header.
	b.WriteString("  ")
	for i := first; i <= last; i++ {
		c := m.res.Columns[i]
		name := c.Name
		switch {
		case tb.IsPrimaryKey(name):
			name += " " + t.Glyphs.Key
		case tb.ForeignKeyFor(name) != nil:
			name += " " + t.Glyphs.Link
		}
		for _, o := range v.order {
			if o.Column == c.Name {
				if o.Desc {
					name += " " + t.Glyphs.Down
				} else {
					name += " " + t.Glyphs.Up
				}
			}
		}
		style := t.Heading
		if i == v.col {
			style = t.Primary.Bold(true)
		}
		cell := ui.PadRight(name, widths[i])
		if render.IsNumeric(c.OID) {
			cell = ui.PadLeft(name, widths[i])
		}
		b.WriteString(style.Render(cell) + "  ")
	}
	b.WriteString("\n")

	rowBg := lipgloss.Color(ui.Blend(t.Palette.CoreDim, "#000000", 0.35))
	cellBg := lipgloss.Color(ui.Blend(t.Palette.CoreFrom, "#000000", 0.45))
	for r, row := range m.res.Rows {
		selected := r == v.row
		marker := "  "
		if selected {
			marker = t.Primary.Bold(true).Render("▌ ")
		}
		b.WriteString(marker)
		for i := first; i <= last; i++ {
			c := m.res.Columns[i]
			text := displayText(row[i], c.OID)
			var cell string
			if render.IsNumeric(c.OID) {
				cell = ui.PadLeft(text, widths[i])
			} else {
				cell = ui.PadRight(text, widths[i])
			}
			style := cellStyle(t, row[i], c.OID)
			if selected && t.Mode.Color {
				style = style.Background(rowBg)
				if i == v.col {
					style = style.Background(cellBg).Bold(true)
				}
			} else if selected && i == v.col {
				style = style.Reverse(true)
			}
			sep := "  "
			if selected && t.Mode.Color && i < last {
				sep = t.R.NewStyle().Background(rowBg).Render("  ")
			}
			b.WriteString(style.Render(cell) + sep)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// displayText is a one-line rendering of a value for the grid.
func displayText(c pg.Cell, oid uint32) string {
	if c.Null {
		return "null"
	}
	if oid == 16 {
		if c.Text == "t" {
			return "true"
		}
		return "false"
	}
	s := strings.ReplaceAll(c.Text, "\n", "↵")
	return ui.Truncate(s, maxColWidth, "…")
}

func cellStyle(t *ui.Theme, c pg.Cell, oid uint32) lipgloss.Style {
	switch {
	case c.Null:
		return t.Null
	case oid == 16:
		if c.Text == "t" {
			return t.Success
		}
		return t.Muted
	case render.IsNumeric(oid):
		return t.Number
	}
	return t.Text
}

func (m *Model) status() string {
	t := m.t
	v := m.top()
	switch m.mode {
	case modeSearch:
		return " " + t.Primary.Bold(true).Render("/ ") + m.input.View()
	case modeFilter:
		return " " + t.Primary.Bold(true).Render("where ") + m.input.View()
	case modeEdit:
		col := m.res.Columns[v.col].Name
		return " " + t.Primary.Bold(true).Render(col+" = ") + m.input.View()
	case modeConfirm:
		sql, _ := updateSQL(v.table, m.pending)
		val := "NULL"
		if m.pending.value != nil {
			val = pg.QuoteLiteral(*m.pending.value)
		}
		return " " + t.ToneGlyph(ui.ToneWarning) + " " + t.Text.Render("set ") + t.Strong.Render(m.pending.column.Name) + t.Text.Render(" = ") + t.Secondary.Render(ui.Truncate(val, 40, "…")) +
			t.Muted.Render("  ·  "+strings.SplitN(sql, " set ", 2)[0]+"  ·  ") + t.Primary.Bold(true).Render("y") + t.Muted.Render(" save  any key cancels")
	}
	if m.flash != "" {
		return " " + t.ToneGlyph(m.flashTone) + " " + t.Tone(m.flashTone).Render(m.flash)
	}
	if m.loading && m.res != nil {
		return " " + tuikit.Spark(t, m.frame) + " " + t.Muted.Render("loading…")
	}
	if m.res == nil {
		return ""
	}
	total := ui.Count(m.total)
	if m.total > rows.CountCap {
		total = "100,000+"
	}
	parts := []string{}
	if len(m.res.Rows) > 0 {
		parts = append(parts, fmt.Sprintf("row %d of %s", v.offset+v.row+1, total))
		pages := (m.total + int64(m.pageSize()) - 1) / int64(m.pageSize())
		parts = append(parts, fmt.Sprintf("page %d/%d", v.offset/m.pageSize()+1, max(pages, 1)))
		col := m.res.Columns[v.col]
		parts = append(parts, col.Name+" "+t.Faint.Render(col.Type))
	} else {
		parts = append(parts, "0 rows")
	}
	parts = append(parts, ui.Duration(m.elapsed))
	return " " + t.Muted.Render(strings.Join(parts, "  ·  "))
}

func (m *Model) hints() string {
	t := m.t
	switch m.mode {
	case modeSearch, modeFilter:
		return tuikit.Hints(t, "enter", "apply", "esc", "cancel", "empty", "clears")
	case modeEdit:
		return tuikit.Hints(t, "enter", "review", "ctrl+n", "set null", "esc", "cancel")
	case modeConfirm:
		return tuikit.Hints(t, "y", "save", "n", "cancel")
	}
	pairs := []string{"↑↓←→", "move", "enter", "inspect", "/", "search", "f", "filter", "s", "sort"}
	if m.hasSelection() && m.top().table.ForeignKeyFor(m.res.Columns[m.top().col].Name) != nil {
		pairs = append(pairs, "g", "follow")
	}
	if !m.o.ReadOnly && m.top().table.Writable() {
		pairs = append(pairs, "e", "edit")
	}
	pairs = append(pairs, "n/p", "page")
	if len(m.stack) > 1 {
		pairs = append(pairs, "esc", "back")
	}
	pairs = append(pairs, "q", "quit")
	return tuikit.Hints(t, pairs...)
}

// openDetail shows the selected row as a record, with JSON pretty-printed.
func (m *Model) openDetail() {
	t := m.t
	v := m.top()
	row := m.res.Rows[v.row]
	kw := 0
	for _, c := range m.res.Columns {
		kw = max(kw, ui.Width(c.Name))
	}
	var b strings.Builder
	for i, c := range m.res.Columns {
		cell := row[i]
		label := t.Key.Render(ui.PadRight(c.Name, kw))
		typ := t.Faint.Render(" " + c.Type)
		var value string
		switch {
		case cell.Null:
			value = t.Null.Render("null")
		case c.OID == 114 || c.OID == 3802:
			value = prettyJSON(t, cell.Text)
		default:
			value = t.Text.Render(ui.Wrap(cell.Text, max(20, m.width-kw-8)))
		}
		if fk := v.table.ForeignKeyFor(c.Name); fk != nil {
			typ += t.Info.Render("  " + t.Glyphs.Link + " " + fk.RefQualified())
		}
		lines := strings.Split(value, "\n")
		b.WriteString(label + "   " + lines[0] + typ + "\n")
		for _, l := range lines[1:] {
			b.WriteString(strings.Repeat(" ", kw+3) + l + "\n")
		}
		if strings.Count(value, "\n") > 0 {
			b.WriteString("\n")
		}
	}
	m.detail.Width, m.detail.Height = m.width-4, m.height-5
	m.detail.SetContent(b.String())
	m.detail.GotoTop()
	m.mode = modeDetail
}

func prettyJSON(t *ui.Theme, s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return t.Text.Render(s)
	}
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		// Colour keys and values separately for readability.
		if i := strings.Index(l, `": `); i > 0 && strings.HasPrefix(strings.TrimSpace(l), `"`) {
			out = append(out, t.Secondary.Render(l[:i+1])+t.Muted.Render(":")+t.Text.Render(l[i+2:]))
		} else {
			out = append(out, t.Text.Render(l))
		}
	}
	return strings.Join(out, "\n")
}

func (m *Model) detailView() string {
	t := m.t
	v := m.top()
	head := " " + t.ToneGlyph(ui.ToneNexus) + " " + t.Title.Render(v.table.DisplayName()) + t.Muted.Render(fmt.Sprintf("  record %d of %s", v.offset+v.row+1, ui.Count(m.total)))
	return head + "\n" + t.Rule(m.width) + "\n" + ui.Indent(m.detail.View(), 2) + "\n" + t.Rule(m.width) + "\n " +
		tuikit.Hints(t, "↑↓", "scroll", "esc", "back")
}

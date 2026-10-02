package ui

import (
	"strings"
)

// Align controls cell alignment.
type Align int

// Alignments.
const (
	AlignLeft Align = iota
	AlignRight
)

// Column describes a table column.
type Column struct {
	Title string
	Align Align
	Min   int // minimum width when shrinking (default 4)
	Max   int // hard cap on width (0 = none)
	// Flex marks a column as the preferred one to shrink when space is tight.
	Flex bool
}

// Table is a width-aware table. Cells may contain ANSI styling.
type Table struct {
	Columns []Column
	Rows    [][]string
	// Width caps the rendered width (0 = terminal content width).
	Width int
	// Indent prefixes every line.
	Indent int
	// Gap between columns (default 2).
	Gap int
	// NoHeader hides the header row.
	NoHeader bool
}

// Table renders t.
//
//	NAME        ROWS     SIZE
//	users       1,204    96 KB
//	sessions    9,812   1.1 MB
func (t *Theme) Table(tb Table) string {
	n := len(tb.Columns)
	if n == 0 {
		return ""
	}
	gap := tb.Gap
	if gap == 0 {
		gap = 2
	}
	widths := make([]int, n)
	for i, c := range tb.Columns {
		if !tb.NoHeader {
			widths[i] = Width(c.Title)
		}
	}
	for _, r := range tb.Rows {
		for i := 0; i < n && i < len(r); i++ {
			if w := Width(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}
	for i, c := range tb.Columns {
		if c.Max > 0 && widths[i] > c.Max {
			widths[i] = c.Max
		}
	}

	limit := tb.Width
	if limit == 0 {
		limit = t.Mode.ContentWidth()
	}
	limit -= tb.Indent
	shrink(widths, tb.Columns, limit-gap*(n-1))

	pad := strings.Repeat(" ", tb.Indent)
	sep := strings.Repeat(" ", gap)
	var b strings.Builder
	writeRow := func(cells []string, style func(i int, s string) string) {
		var line strings.Builder
		line.WriteString(pad)
		for i := 0; i < n; i++ {
			cell := ""
			if i < len(cells) {
				cell = strings.ReplaceAll(cells[i], "\n", " ")
			}
			var s string
			if tb.Columns[i].Align == AlignRight {
				s = PadLeft(cell, widths[i])
			} else if i == n-1 {
				s = Truncate(cell, widths[i], "…")
			} else {
				s = PadRight(cell, widths[i])
			}
			if style != nil {
				s = style(i, s)
			}
			line.WriteString(s)
			if i < n-1 {
				line.WriteString(sep)
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}

	if !tb.NoHeader {
		titles := make([]string, n)
		for i, c := range tb.Columns {
			titles[i] = c.Title
			if t.Mode.Plain {
				titles[i] = strings.ToUpper(c.Title)
			}
		}
		writeRow(titles, func(_ int, s string) string { return t.Heading.Render(s) })
	}
	for _, r := range tb.Rows {
		writeRow(r, nil)
	}
	return strings.TrimRight(b.String(), "\n")
}

// shrink reduces column widths until their sum fits within limit, taking
// from flexible and then from the widest columns first.
func shrink(widths []int, cols []Column, limit int) {
	total := 0
	for _, w := range widths {
		total += w
	}
	for total > limit {
		idx := -1
		for i := range widths {
			min := cols[i].Min
			if min == 0 {
				min = 4
			}
			if widths[i] <= min {
				continue
			}
			if idx == -1 {
				idx = i
				continue
			}
			// Prefer flexible columns, then the widest.
			if cols[i].Flex != cols[idx].Flex {
				if cols[i].Flex {
					idx = i
				}
				continue
			}
			if widths[i] > widths[idx] {
				idx = i
			}
		}
		if idx == -1 {
			return
		}
		widths[idx]--
		total--
	}
}

// KV renders aligned key/value pairs:
//
//	size          12.4 MB
//	connections   4 / 100
type KV struct {
	Pairs  [][2]string
	Indent int
	KeyMin int
}

// KV renders a key/value list.
func (t *Theme) KV(kv KV) string {
	kw := kv.KeyMin
	for _, p := range kv.Pairs {
		if w := Width(p[0]); w > kw {
			kw = w
		}
	}
	pad := strings.Repeat(" ", kv.Indent)
	var b strings.Builder
	for i, p := range kv.Pairs {
		b.WriteString(pad)
		b.WriteString(t.Key.Render(PadRight(p[0], kw)))
		b.WriteString("   ")
		b.WriteString(p[1])
		if i < len(kv.Pairs)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TreeNode is a node in a rendered tree.
type TreeNode struct {
	Label    string
	Children []TreeNode
}

// Tree renders nodes with box-drawing branches:
//
//	├─ users
//	│  └─ email
//	└─ sessions
func (t *Theme) Tree(nodes []TreeNode) string {
	var b strings.Builder
	t.tree(&b, nodes, "")
	return strings.TrimRight(b.String(), "\n")
}

func (t *Theme) tree(b *strings.Builder, nodes []TreeNode, prefix string) {
	g := t.Glyphs
	for i, n := range nodes {
		last := i == len(nodes)-1
		branch := g.TreeMid
		next := prefix + t.Faint.Render(g.TreePipe) + "  "
		if last {
			branch = g.TreeEnd
			next = prefix + "   "
		}
		b.WriteString(prefix)
		b.WriteString(t.Faint.Render(branch))
		b.WriteString(" ")
		b.WriteString(n.Label)
		b.WriteString("\n")
		if len(n.Children) > 0 {
			t.tree(b, n.Children, next)
		}
	}
}

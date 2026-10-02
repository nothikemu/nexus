package explain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/textutil"
)

// Severity of an insight.
type Severity string

// Severities, from most to least urgent.
const (
	Warning Severity = "warning"
	Info    Severity = "info"
	Good    Severity = "good"
)

// Insight is a finding about a plan.
type Insight struct {
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	SQL      string   `json:"sql,omitempty"` // a statement that may help
	Node     string   `json:"node,omitempty"`
}

// Thresholds below which scans are considered cheap regardless of shape.
const (
	minRowsForIndex   = 1000
	misestimateFactor = 10.0
)

// columnRefRe finds column references on the left of comparisons in a
// filter expression, e.g. "(email = 'x'::text)" or "(u.org_id = 4)".
var columnRefRe = regexp.MustCompile(`\(\s*(?:"?[A-Za-z_][A-Za-z0-9_$]*"?\.)?"?([A-Za-z_][A-Za-z0-9_$]*)"?\s*(?:=|<>|!=|<=|>=|<|>|~~\*?|!~~\*?|IS\s+(?:NOT\s+)?NULL|= ANY)`)

// lowerRefRe finds lower(col) comparisons, which need an expression index.
var lowerRefRe = regexp.MustCompile(`lower\(\s*\(?(?:"?[A-Za-z_][A-Za-z0-9_$]*"?\.)?"?([A-Za-z_][A-Za-z0-9_$]*)"?\)?(?:::text)?\s*\)\s*=`)

// Analyze inspects a plan and returns findings, most urgent first. snap may
// be nil, in which case index suggestions are skipped (Nexus never guesses
// at columns it hasn't seen).
func Analyze(p *Plan, snap *introspect.Snapshot) []Insight {
	var out []Insight
	seen := map[string]bool{}
	add := func(in Insight) {
		key := in.Title + in.SQL
		if !seen[key] {
			seen[key] = true
			out = append(out, in)
		}
	}

	p.Walk(func(n *Node, _ int) {
		switch {
		case n.Type == "Seq Scan":
			seqScan(p, n, snap, add)
		case n.Type == "Index Scan" || n.Type == "Index Only Scan" || n.Type == "Bitmap Index Scan":
			add(Insight{Severity: Good, Title: "uses index " + n.Index, Node: n.Label()})
		}
		if p.Analyzed {
			misestimate(n, add)
		}
		if n.SortSpaceType == "Disk" {
			add(Insight{
				Severity: Warning,
				Title:    fmt.Sprintf("sort spilled %s to disk", kb(n.SortSpaceUsed)),
				Detail:   "the sort didn't fit in work_mem. Raising it for this session (or adding an index that matches the ORDER BY) keeps it in memory.",
				SQL:      fmt.Sprintf("set work_mem = '%dMB';", workMemFor(n.SortSpaceUsed)),
				Node:     n.Label(),
			})
		}
		if n.Type == "Hash" && n.HashBatches > 1 {
			add(Insight{
				Severity: Info,
				Title:    fmt.Sprintf("hash table split into %.0f batches", n.HashBatches),
				Detail:   "the hash join's table didn't fit in work_mem, so it was processed in batches on disk.",
				SQL:      fmt.Sprintf("set work_mem = '%dMB';", workMemFor(n.PeakMemory*n.HashBatches)),
				Node:     n.Label(),
			})
		}
	})
	if !p.Analyzed {
		add(Insight{Severity: Info, Title: "these are the planner's estimates", Detail: "add --analyze to run the query and measure it. Nexus wraps it in a transaction and rolls back, so writes leave no trace."})
	}
	order := map[Severity]int{Warning: 0, Info: 1, Good: 2}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Severity] < order[out[j].Severity] })
	return out
}

func seqScan(p *Plan, n *Node, snap *introspect.Snapshot, add func(Insight)) {
	read := n.RowsRead(p.Analyzed)
	var table *introspect.Table
	if snap != nil && n.Relation != "" {
		name := n.Relation
		if n.Schema != "" {
			name = n.Schema + "." + n.Relation
		}
		table = snap.Table(name)
	}
	if table != nil && !p.Analyzed && float64(table.Rows) > read {
		read = float64(table.Rows)
	}
	if read < minRowsForIndex || n.Filter == "" {
		return
	}
	returned := n.RowsOut(p.Analyzed)
	selective := returned < read/2
	if !selective {
		return
	}
	scanned := fmt.Sprintf("scanned %s rows to return %s", count(read), count(returned))
	if table == nil {
		add(Insight{Severity: Warning, Title: "sequential scan on " + n.Relation, Detail: scanned + ". An index on the filtered column may help.", Node: n.Label()})
		return
	}

	cols, exprs := filterColumns(n.Filter, table)
	if len(cols) == 0 && len(exprs) == 0 {
		add(Insight{Severity: Warning, Title: "sequential scan on " + table.DisplayName(), Detail: scanned + " (filter: " + n.Filter + ").", Node: n.Label()})
		return
	}
	for _, col := range cols {
		if ix := leadingIndex(table, col); ix != nil {
			add(Insight{
				Severity: Info,
				Title:    fmt.Sprintf("index %s exists but wasn't used", ix.Name),
				Detail:   scanned + ". The planner may have stale statistics, or the filter matches too many rows for the index to pay off.",
				SQL:      "analyze " + pg.QuoteIdent(table.Schema, table.Name) + ";",
				Node:     n.Label(),
			})
			continue
		}
		add(Insight{
			Severity: Warning,
			Title:    fmt.Sprintf("%s has no index on %s", table.DisplayName(), col),
			Detail:   scanned + ". An index on " + col + " lets PostgreSQL jump straight to matching rows.",
			SQL:      fmt.Sprintf("create index concurrently %s on %s (%s);", indexName(table.Name, col), pg.QuoteIdent(table.Schema, table.Name), pg.QuoteIdent(col)),
			Node:     n.Label(),
		})
	}
	for _, col := range exprs {
		add(Insight{
			Severity: Warning,
			Title:    fmt.Sprintf("%s has no index on lower(%s)", table.DisplayName(), col),
			Detail:   scanned + ". Filtering on lower(" + col + ") needs an expression index; a plain index on " + col + " can't be used.",
			SQL:      fmt.Sprintf("create index concurrently %s on %s (lower(%s));", indexName(table.Name, col+"_lower"), pg.QuoteIdent(table.Schema, table.Name), pg.QuoteIdent(col)),
			Node:     n.Label(),
		})
	}
}

// filterColumns returns real columns of table referenced by a filter, split
// into plain column comparisons and lower(col) comparisons.
func filterColumns(filter string, table *introspect.Table) (cols, lowered []string) {
	seen := map[string]bool{}
	for _, m := range lowerRefRe.FindAllStringSubmatch(filter, -1) {
		if table.Column(m[1]) != nil && !seen["lower:"+m[1]] {
			seen["lower:"+m[1]] = true
			lowered = append(lowered, m[1])
		}
	}
	for _, m := range columnRefRe.FindAllStringSubmatch(filter, -1) {
		if table.Column(m[1]) != nil && !seen[m[1]] && !seen["lower:"+m[1]] {
			seen[m[1]] = true
			cols = append(cols, m[1])
		}
	}
	return cols, lowered
}

// leadingIndex returns a valid index whose first key column is col.
func leadingIndex(t *introspect.Table, col string) *introspect.Index {
	for _, ix := range t.Indexes {
		if ix.Valid && len(ix.Columns) > 0 && strings.Trim(ix.Columns[0], `"`) == col {
			return ix
		}
	}
	return nil
}

func misestimate(n *Node, add func(Insight)) {
	actual := n.ActualRows
	est := n.PlanRows
	if actual < minRowsForIndex && est < minRowsForIndex {
		return
	}
	lo, hi := actual, est
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 1 {
		lo = 1
	}
	if hi/lo < misestimateFactor {
		return
	}
	in := Insight{
		Severity: Info,
		Title:    fmt.Sprintf("row estimate off by %.0f×", hi/lo),
		Detail:   fmt.Sprintf("the planner expected %s rows but got %s. Fresh statistics lead to better plans.", count(est), count(actual)),
		Node:     n.Label(),
	}
	if n.Relation != "" {
		rel := n.Relation
		if n.Schema != "" {
			rel = pg.QuoteIdent(n.Schema, n.Relation)
		}
		in.SQL = "analyze " + rel + ";"
	}
	add(in)
}

func indexName(table, col string) string {
	name := table + "_" + col + "_idx"
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func workMemFor(kbUsed float64) int {
	mb := int(kbUsed/1024*2) + 1
	for _, step := range []int{16, 32, 64, 128, 256, 512, 1024} {
		if mb <= step {
			return step
		}
	}
	return 1024
}

func kb(v float64) string {
	if v >= 1024 {
		return fmt.Sprintf("%.1f MB", v/1024)
	}
	return fmt.Sprintf("%.0f kB", v)
}

func count(f float64) string { return textutil.Thousands(int64(f + 0.5)) }

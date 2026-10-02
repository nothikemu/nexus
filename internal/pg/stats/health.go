package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
)

// Severity of a health finding.
type Severity string

// Severities.
const (
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Finding is a schema health problem.
type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Objects  []string `json:"objects"`
	Fix      []string `json:"fix,omitempty"` // statements that would resolve it
}

// Health evaluates a snapshot for common schema problems. It is pure: all
// inputs come from the snapshot, so results are reproducible and testable.
func Health(s *introspect.Snapshot) []Finding {
	var out []Finding
	tables := s.BaseTables()

	// Tables without a primary key.
	var noPK []string
	for _, t := range tables {
		if len(t.PrimaryKey) == 0 {
			noPK = append(noPK, t.DisplayName())
		}
	}
	if len(noPK) > 0 {
		out = append(out, Finding{
			Severity: SevWarning, Code: "no_primary_key",
			Title:   plural(len(noPK), "table has", "tables have") + " no primary key",
			Detail:  "rows can't be addressed reliably: editing, logical replication and generated APIs all need one.",
			Objects: noPK,
		})
	}

	// Foreign keys without a supporting index.
	var fkObjs, fkFix []string
	for _, t := range tables {
		for _, fk := range t.ForeignKeys {
			if !indexed(t, fk.Columns) {
				fkObjs = append(fkObjs, fmt.Sprintf("%s(%s)", t.DisplayName(), strings.Join(fk.Columns, ", ")))
				fkFix = append(fkFix, fmt.Sprintf("create index concurrently %s on %s (%s);",
					truncIdent(t.Name+"_"+strings.Join(fk.Columns, "_")+"_idx"), pg.QuoteIdent(t.Schema, t.Name), quoteCols(fk.Columns)))
			}
		}
	}
	if len(fkObjs) > 0 {
		out = append(out, Finding{
			Severity: SevWarning, Code: "unindexed_foreign_key",
			Title:   plural(len(fkObjs), "foreign key has", "foreign keys have") + " no index",
			Detail:  "joins on these columns, and deletes on the referenced table, must scan the whole table.",
			Objects: fkObjs, Fix: fkFix,
		})
	}

	// Duplicate and invalid indexes.
	var dupObjs, dupFix, invalid, invalidFix []string
	for _, t := range s.Tables {
		seen := map[string]string{}
		for _, ix := range t.Indexes {
			if !ix.Valid {
				invalid = append(invalid, t.DisplayName()+"."+ix.Name)
				invalidFix = append(invalidFix, "drop index concurrently "+pg.QuoteIdent(t.Schema, ix.Name)+";")
				continue
			}
			key := ix.Method + "|" + strings.Join(ix.Columns, ",") + "|" + ix.Predicate
			if otherName, ok := seen[key]; ok {
				other := findIndex(t, otherName)
				// Drop the plain index; constraint-backed ones must stay.
				var drop *introspect.Index
				switch {
				case !ix.Unique && !ix.Primary:
					drop = ix
				case !other.Unique && !other.Primary:
					drop = other
					seen[key] = ix.Name
				}
				dupObjs = append(dupObjs, fmt.Sprintf("%s: %s = %s", t.DisplayName(), otherName, ix.Name))
				if drop != nil {
					dupFix = append(dupFix, "drop index concurrently "+pg.QuoteIdent(t.Schema, drop.Name)+";")
				}
				continue
			}
			seen[key] = ix.Name
		}
	}
	if len(dupObjs) > 0 {
		out = append(out, Finding{
			Severity: SevWarning, Code: "duplicate_index",
			Title:   plural(len(dupObjs), "duplicate index", "duplicate indexes"),
			Detail:  "identical indexes cost writes and disk while adding nothing for reads.",
			Objects: dupObjs, Fix: dupFix,
		})
	}
	if len(invalid) > 0 {
		out = append(out, Finding{
			Severity: SevWarning, Code: "invalid_index",
			Title:   plural(len(invalid), "invalid index", "invalid indexes"),
			Detail:  "usually left behind by a failed CREATE INDEX CONCURRENTLY. They're maintained on every write but never used.",
			Objects: invalid, Fix: invalidFix,
		})
	}

	// Dead tuples.
	var bloat, bloatFix []string
	for _, t := range tables {
		dead, live := t.Stats.DeadTuples, t.Stats.LiveTuples
		if dead > 10000 && float64(dead) > 0.2*float64(dead+live) {
			bloat = append(bloat, fmt.Sprintf("%s (%d%% dead)", t.DisplayName(), dead*100/(dead+live)))
			bloatFix = append(bloatFix, "vacuum (analyze) "+pg.QuoteIdent(t.Schema, t.Name)+";")
		}
	}
	if len(bloat) > 0 {
		out = append(out, Finding{
			Severity: SevInfo, Code: "dead_tuples",
			Title:   plural(len(bloat), "table needs", "tables need") + " vacuuming",
			Detail:  "a large share of their rows are dead versions that still cost space and scan time.",
			Objects: bloat, Fix: bloatFix,
		})
	}

	// Sequences close to their maximum.
	var seqs []string
	for _, sq := range s.Sequences {
		if sq.LastValue != nil && sq.MaxValue > 0 && float64(*sq.LastValue) > 0.8*float64(sq.MaxValue) {
			seqs = append(seqs, fmt.Sprintf("%s.%s (%s, %d%% used)", sq.Schema, sq.Name, sq.DataType, *sq.LastValue*100/sq.MaxValue))
		}
	}
	if len(seqs) > 0 {
		out = append(out, Finding{
			Severity: SevWarning, Code: "sequence_exhaustion",
			Title:   plural(len(seqs), "sequence is", "sequences are") + " running out of values",
			Detail:  "inserts will fail once the sequence reaches its maximum. Migrate the column to bigint.",
			Objects: seqs,
		})
	}

	// Large indexes that have never been used on busy tables.
	var unused []string
	for _, t := range tables {
		if t.Stats.SeqScans+t.Stats.IdxScans < 1000 {
			continue
		}
		for _, ix := range t.Indexes {
			if ix.Scans == 0 && !ix.Unique && !ix.Primary && ix.Bytes >= 10<<20 {
				unused = append(unused, t.DisplayName()+"."+ix.Name)
			}
		}
	}
	if len(unused) > 0 {
		out = append(out, Finding{
			Severity: SevInfo, Code: "unused_index",
			Title:   plural(len(unused), "large index has", "large indexes have") + " never been used",
			Detail:  "since statistics were last reset. Confirm on production before dropping.",
			Objects: unused,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity == SevWarning && out[j].Severity != SevWarning })
	return out
}

// indexed reports whether some valid index has cols as its leading columns.
func indexed(t *introspect.Table, cols []string) bool {
	for _, ix := range t.Indexes {
		if !ix.Valid || ix.Predicate != "" || len(ix.Columns) < len(cols) {
			continue
		}
		match := true
		for i, c := range cols {
			if strings.Trim(ix.Columns[i], `"`) != c {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func findIndex(t *introspect.Table, name string) *introspect.Index {
	for _, ix := range t.Indexes {
		if ix.Name == name {
			return ix
		}
	}
	return nil
}

func quoteCols(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = pg.QuoteIdent(c)
	}
	return strings.Join(q, ", ")
}

func truncIdent(s string) string {
	if len(s) > 63 {
		return s[:63]
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Package schemadiff compares two introspection snapshots and reports the
// structural changes between them. Nexus uses it to preview migrations: it
// snapshots the schema, applies pending migrations inside a transaction,
// snapshots again, diffs, and rolls back.
package schemadiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nothikemu/nexus/internal/pg/introspect"
)

// Kind is the type of change.
type Kind string

// Change kinds.
const (
	Added   Kind = "added"
	Removed Kind = "removed"
	Changed Kind = "changed"
)

// Change is one structural difference.
type Change struct {
	Kind   Kind   `json:"kind"`
	Object string `json:"object"`           // table, column, index, constraint, foreign key, policy, trigger, rls, view, function, enum, sequence, extension
	Name   string `json:"name"`             // object name (qualified for top-level objects)
	Parent string `json:"parent,omitempty"` // owning table for sub-objects
	Detail string `json:"detail,omitempty"` // what changed, e.g. "text → varchar(255)"
}

// Diff is the full set of changes, ordered by parent then object.
type Diff struct {
	Changes []Change `json:"changes"`
}

// Empty reports whether nothing changed.
func (d Diff) Empty() bool { return len(d.Changes) == 0 }

// TablesTouched counts distinct tables and views affected.
func (d Diff) TablesTouched() int {
	seen := map[string]bool{}
	for _, c := range d.Changes {
		switch {
		case c.Parent != "":
			seen[c.Parent] = true
		case c.Object == "table" || c.Object == "view" || c.Object == "materialized view":
			seen[c.Name] = true
		}
	}
	return len(seen)
}

// Count returns the number of changes of a kind.
func (d Diff) Count(k Kind) int {
	n := 0
	for _, c := range d.Changes {
		if c.Kind == k {
			n++
		}
	}
	return n
}

// Compare returns the changes needed to go from a to b.
func Compare(a, b *introspect.Snapshot) Diff {
	var d diffBuilder
	d.tables(a.Tables, b.Tables)
	d.functions(a.Functions, b.Functions)
	d.enums(a.Enums, b.Enums)
	d.sequences(a.Sequences, b.Sequences)
	d.extensions(a.Extensions, b.Extensions)
	sort.SliceStable(d.out, func(i, j int) bool {
		ki, kj := sortKey(d.out[i]), sortKey(d.out[j])
		return ki < kj
	})
	return Diff{Changes: d.out}
}

func sortKey(c Change) string {
	group := c.Parent
	if group == "" {
		group = c.Name
	}
	sub := "1"
	if c.Parent == "" {
		sub = "0"
	}
	return group + "\x00" + sub + "\x00" + c.Object + "\x00" + c.Name
}

type diffBuilder struct {
	out []Change
}

func (d *diffBuilder) add(c Change) { d.out = append(d.out, c) }

func (d *diffBuilder) tables(a, b []*introspect.Table) {
	am, bm := indexTables(a), indexTables(b)
	for name, bt := range bm {
		at, ok := am[name]
		if !ok {
			d.add(Change{Kind: Added, Object: bt.Kind, Name: name, Detail: plural(len(bt.Columns), "column")})
			continue
		}
		if at.Kind != bt.Kind {
			d.add(Change{Kind: Changed, Object: bt.Kind, Name: name, Detail: at.Kind + " → " + bt.Kind})
		}
		d.table(name, at, bt)
	}
	for name, at := range am {
		if _, ok := bm[name]; !ok {
			d.add(Change{Kind: Removed, Object: at.Kind, Name: name})
		}
	}
}

func indexTables(ts []*introspect.Table) map[string]*introspect.Table {
	m := make(map[string]*introspect.Table, len(ts))
	for _, t := range ts {
		m[t.QualifiedName()] = t
	}
	return m
}

func (d *diffBuilder) table(parent string, a, b *introspect.Table) {
	// Columns
	ac, bc := map[string]*introspect.Column{}, map[string]*introspect.Column{}
	for _, c := range a.Columns {
		ac[c.Name] = c
	}
	for _, c := range b.Columns {
		bc[c.Name] = c
	}
	for _, c := range b.Columns {
		old, ok := ac[c.Name]
		if !ok {
			d.add(Change{Kind: Added, Object: "column", Name: c.Name, Parent: parent, Detail: columnSummary(c)})
			continue
		}
		var changes []string
		if old.Type != c.Type {
			changes = append(changes, "type "+old.Type+" → "+c.Type)
		}
		if old.Nullable != c.Nullable {
			changes = append(changes, nullability(old.Nullable)+" → "+nullability(c.Nullable))
		}
		if old.Default != c.Default {
			changes = append(changes, "default "+orNone(old.Default)+" → "+orNone(c.Default))
		}
		if old.Generated != c.Generated {
			changes = append(changes, "generated "+orNone(old.Generated)+" → "+orNone(c.Generated))
		}
		if old.Identity != c.Identity {
			changes = append(changes, "identity "+orNone(old.Identity)+" → "+orNone(c.Identity))
		}
		if len(changes) > 0 {
			d.add(Change{Kind: Changed, Object: "column", Name: c.Name, Parent: parent, Detail: strings.Join(changes, ", ")})
		}
	}
	for _, c := range a.Columns {
		if _, ok := bc[c.Name]; !ok {
			d.add(Change{Kind: Removed, Object: "column", Name: c.Name, Parent: parent})
		}
	}

	// Indexes (by name; definition changes count as changed).
	named(d, parent, "index",
		mapBy(a.Indexes, func(i *introspect.Index) (string, string) { return i.Name, i.Definition }),
		mapBy(b.Indexes, func(i *introspect.Index) (string, string) { return i.Name, i.Definition }))
	named(d, parent, "constraint",
		mapBy(a.Constraints, func(c *introspect.Constraint) (string, string) { return c.Name, c.Definition }),
		mapBy(b.Constraints, func(c *introspect.Constraint) (string, string) { return c.Name, c.Definition }))
	named(d, parent, "foreign key",
		mapBy(a.ForeignKeys, func(f *introspect.ForeignKey) (string, string) { return f.Name, f.Definition }),
		mapBy(b.ForeignKeys, func(f *introspect.ForeignKey) (string, string) { return f.Name, f.Definition }))
	named(d, parent, "policy",
		mapBy(a.Policies, policyKey), mapBy(b.Policies, policyKey))
	named(d, parent, "trigger",
		mapBy(a.Triggers, func(t *introspect.Trigger) (string, string) { return t.Name, t.Definition }),
		mapBy(b.Triggers, func(t *introspect.Trigger) (string, string) { return t.Name, t.Definition }))

	if a.RLSEnabled != b.RLSEnabled {
		d.add(Change{Kind: Changed, Object: "rls", Name: "row level security", Parent: parent, Detail: onOff(a.RLSEnabled) + " → " + onOff(b.RLSEnabled)})
	}
	if a.IsView() && b.IsView() && a.Definition != b.Definition {
		d.add(Change{Kind: Changed, Object: "definition", Name: "query", Parent: parent, Detail: "view definition changed"})
	}
	if a.Comment != b.Comment {
		d.add(Change{Kind: Changed, Object: "comment", Name: "comment", Parent: parent, Detail: orNone(quote(a.Comment)) + " → " + orNone(quote(b.Comment))})
	}
}

func policyKey(p *introspect.Policy) (string, string) {
	return p.Name, fmt.Sprintf("%s %v %v %s %s", p.Command, p.Permissive, p.Roles, p.Using, p.Check)
}

func mapBy[T any](items []T, key func(T) (string, string)) map[string]string {
	m := make(map[string]string, len(items))
	for _, it := range items {
		k, v := key(it)
		m[k] = v
	}
	return m
}

// named diffs two name→definition maps.
func named(d *diffBuilder, parent, object string, a, b map[string]string) {
	for _, name := range sortedKeys(b) {
		def := b[name]
		old, ok := a[name]
		switch {
		case !ok:
			d.add(Change{Kind: Added, Object: object, Name: name, Parent: parent, Detail: def})
		case old != def:
			d.add(Change{Kind: Changed, Object: object, Name: name, Parent: parent, Detail: def})
		}
	}
	for _, name := range sortedKeys(a) {
		if _, ok := b[name]; !ok {
			d.add(Change{Kind: Removed, Object: object, Name: name, Parent: parent})
		}
	}
}

func (d *diffBuilder) functions(a, b []*introspect.Function) {
	key := func(f *introspect.Function) (string, string) {
		return f.Signature(), f.Returns + "|" + f.Language + "|" + f.Volatility + "|" + fmt.Sprint(f.SecurityDefiner) + "|" + f.BodyHash
	}
	am, bm := mapBy(a, key), mapBy(b, key)
	kinds := map[string]string{}
	for _, f := range append(append([]*introspect.Function{}, a...), b...) {
		kinds[f.Signature()] = f.Kind
	}
	for _, sig := range sortedKeys(bm) {
		if old, ok := am[sig]; !ok {
			d.add(Change{Kind: Added, Object: kinds[sig], Name: sig})
		} else if old != bm[sig] {
			d.add(Change{Kind: Changed, Object: kinds[sig], Name: sig, Detail: "definition changed"})
		}
	}
	for _, sig := range sortedKeys(am) {
		if _, ok := bm[sig]; !ok {
			d.add(Change{Kind: Removed, Object: kinds[sig], Name: sig})
		}
	}
}

func (d *diffBuilder) enums(a, b []*introspect.Enum) {
	key := func(e *introspect.Enum) (string, string) {
		return e.Schema + "." + e.Name, strings.Join(e.Values, ", ")
	}
	am, bm := mapBy(a, key), mapBy(b, key)
	for _, name := range sortedKeys(bm) {
		if old, ok := am[name]; !ok {
			d.add(Change{Kind: Added, Object: "enum", Name: name, Detail: bm[name]})
		} else if old != bm[name] {
			d.add(Change{Kind: Changed, Object: "enum", Name: name, Detail: old + " → " + bm[name]})
		}
	}
	for _, name := range sortedKeys(am) {
		if _, ok := bm[name]; !ok {
			d.add(Change{Kind: Removed, Object: "enum", Name: name})
		}
	}
}

func (d *diffBuilder) sequences(a, b []*introspect.Sequence) {
	key := func(s *introspect.Sequence) (string, string) { return s.Schema + "." + s.Name, s.DataType }
	am, bm := mapBy(a, key), mapBy(b, key)
	for _, name := range sortedKeys(bm) {
		if _, ok := am[name]; !ok {
			d.add(Change{Kind: Added, Object: "sequence", Name: name})
		}
	}
	for _, name := range sortedKeys(am) {
		if _, ok := bm[name]; !ok {
			d.add(Change{Kind: Removed, Object: "sequence", Name: name})
		}
	}
}

func (d *diffBuilder) extensions(a, b []*introspect.Extension) {
	key := func(e *introspect.Extension) (string, string) { return e.Name, e.Version }
	am, bm := mapBy(a, key), mapBy(b, key)
	for _, name := range sortedKeys(bm) {
		if old, ok := am[name]; !ok {
			d.add(Change{Kind: Added, Object: "extension", Name: name, Detail: bm[name]})
		} else if old != bm[name] {
			d.add(Change{Kind: Changed, Object: "extension", Name: name, Detail: old + " → " + bm[name]})
		}
	}
	for _, name := range sortedKeys(am) {
		if _, ok := bm[name]; !ok {
			d.add(Change{Kind: Removed, Object: "extension", Name: name})
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func columnSummary(c *introspect.Column) string {
	s := c.Type
	if !c.Nullable {
		s += " not null"
	}
	if c.Default != "" {
		s += " default " + c.Default
	}
	if c.Identity != "" {
		s += " identity"
	}
	if c.Generated != "" {
		s += " generated"
	}
	return s
}

func nullability(nullable bool) string {
	if nullable {
		return "nullable"
	}
	return "not null"
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func quote(s string) string {
	if s == "" {
		return ""
	}
	return "'" + s + "'"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

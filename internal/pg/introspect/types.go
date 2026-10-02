// Package introspect reads PostgreSQL's catalogs into a structured snapshot
// of a database's schema. Snapshots power the table explorer, `nexus db
// schema`, health checks and migration diffs.
package introspect

import (
	"strings"

	"github.com/nothikemu/nexus/internal/textutil"
)

// Snapshot is the schema of a set of PostgreSQL schemas at a point in time.
type Snapshot struct {
	Schemas    []string     `json:"schemas"`
	Tables     []*Table     `json:"tables"`
	Functions  []*Function  `json:"functions"`
	Enums      []*Enum      `json:"enums"`
	Sequences  []*Sequence  `json:"sequences"`
	Extensions []*Extension `json:"extensions"`
}

// Relation kinds.
const (
	KindTable       = "table"
	KindPartitioned = "partitioned table"
	KindView        = "view"
	KindMatView     = "materialized view"
	KindForeign     = "foreign table"
)

// Table is a relation: table, partitioned table, view, materialized view or
// foreign table.
type Table struct {
	Schema       string        `json:"schema"`
	Name         string        `json:"name"`
	Kind         string        `json:"kind"`
	Comment      string        `json:"comment,omitempty"`
	Columns      []*Column     `json:"columns"`
	PrimaryKey   []string      `json:"primary_key,omitempty"`
	Indexes      []*Index      `json:"indexes,omitempty"`
	Constraints  []*Constraint `json:"constraints,omitempty"`
	ForeignKeys  []*ForeignKey `json:"foreign_keys,omitempty"`
	ReferencedBy []*ForeignKey `json:"referenced_by,omitempty"`
	Policies     []*Policy     `json:"policies,omitempty"`
	Triggers     []*Trigger    `json:"triggers,omitempty"`
	RLSEnabled   bool          `json:"rls_enabled"`
	RLSForced    bool          `json:"rls_forced"`
	Rows         int64         `json:"estimated_rows"`
	Bytes        int64         `json:"total_bytes"`
	Definition   string        `json:"definition,omitempty"`   // views
	PartitionOf  string        `json:"partition_of,omitempty"` // parent, for partitions
	Stats        TableStats    `json:"stats"`
}

// TableStats are cumulative activity counters from pg_stat_user_tables.
type TableStats struct {
	SeqScans    int64 `json:"seq_scans"`
	IdxScans    int64 `json:"idx_scans"`
	DeadTuples  int64 `json:"dead_tuples"`
	LiveTuples  int64 `json:"live_tuples"`
	HasVacuumed bool  `json:"has_vacuumed"`
	HasAnalyzed bool  `json:"has_analyzed"`
}

// QualifiedName is schema.name.
func (t *Table) QualifiedName() string { return t.Schema + "." + t.Name }

// DisplayName omits the public schema.
func (t *Table) DisplayName() string {
	if t.Schema == "public" {
		return t.Name
	}
	return t.QualifiedName()
}

// IsView reports whether the relation is a view or materialized view.
func (t *Table) IsView() bool { return t.Kind == KindView || t.Kind == KindMatView }

// Writable reports whether rows can be edited by primary key.
func (t *Table) Writable() bool {
	return (t.Kind == KindTable || t.Kind == KindPartitioned) && len(t.PrimaryKey) > 0
}

// Column returns a column by name.
func (t *Table) Column(name string) *Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// IsPrimaryKey reports whether a column is part of the primary key.
func (t *Table) IsPrimaryKey(col string) bool {
	for _, c := range t.PrimaryKey {
		if c == col {
			return true
		}
	}
	return false
}

// ForeignKeyFor returns the single-column foreign key on col, if any.
func (t *Table) ForeignKeyFor(col string) *ForeignKey {
	for _, fk := range t.ForeignKeys {
		if len(fk.Columns) == 1 && fk.Columns[0] == col {
			return fk
		}
	}
	return nil
}

// IsUnique reports whether a single-column unique constraint or index covers col.
func (t *Table) IsUnique(col string) bool {
	for _, ix := range t.Indexes {
		if ix.Unique && !ix.Primary && len(ix.Columns) == 1 && ix.Columns[0] == col && ix.Predicate == "" {
			return true
		}
	}
	return false
}

// Column is a table column.
type Column struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Nullable  bool   `json:"nullable"`
	Default   string `json:"default,omitempty"`
	Generated string `json:"generated,omitempty"` // generation expression for stored generated columns
	Identity  string `json:"identity,omitempty"`  // "always" | "by default"
	Comment   string `json:"comment,omitempty"`
	Position  int    `json:"position"`
	IsEnum    bool   `json:"is_enum,omitempty"`
}

// Index is an index on a table.
type Index struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"` // key columns or expressions
	Unique     bool     `json:"unique"`
	Primary    bool     `json:"primary"`
	Method     string   `json:"method"`
	Definition string   `json:"definition"`
	Predicate  string   `json:"predicate,omitempty"`
	Bytes      int64    `json:"bytes"`
	Scans      int64    `json:"scans"`
	Valid      bool     `json:"valid"`
}

// Constraint is a check, unique, or exclusion constraint.
type Constraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // check | unique | exclusion | primary key
	Definition string   `json:"definition"`
	Columns    []string `json:"columns,omitempty"`
}

// ForeignKey is a foreign key relationship.
type ForeignKey struct {
	Name       string   `json:"name"`
	Schema     string   `json:"schema"`
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"ref_schema"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	OnDelete   string   `json:"on_delete"`
	OnUpdate   string   `json:"on_update"`
	Definition string   `json:"definition"`
}

// RefQualified is the referenced table as schema.name.
func (f *ForeignKey) RefQualified() string { return f.RefSchema + "." + f.RefTable }

// Qualified is the referencing table as schema.name.
func (f *ForeignKey) Qualified() string { return f.Schema + "." + f.Table }

// Policy is a row-level security policy.
type Policy struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"` // ALL | SELECT | INSERT | UPDATE | DELETE
	Permissive bool     `json:"permissive"`
	Roles      []string `json:"roles"`
	Using      string   `json:"using,omitempty"`
	Check      string   `json:"check,omitempty"`
}

// Trigger is a user trigger.
type Trigger struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Enabled    bool   `json:"enabled"`
}

// Function is a function, procedure, aggregate or window function.
type Function struct {
	Schema          string `json:"schema"`
	Name            string `json:"name"`
	Arguments       string `json:"arguments"`
	Returns         string `json:"returns,omitempty"`
	Language        string `json:"language"`
	Kind            string `json:"kind"`
	Volatility      string `json:"volatility"`
	SecurityDefiner bool   `json:"security_definer"`
	BodyHash        string `json:"-"`
}

// Signature is name(args).
func (f *Function) Signature() string { return f.Schema + "." + f.Name + "(" + f.Arguments + ")" }

// Enum is an enumerated type.
type Enum struct {
	Schema string   `json:"schema"`
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Sequence is a sequence generator.
type Sequence struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	DataType  string `json:"data_type"`
	LastValue *int64 `json:"last_value"`
	MaxValue  int64  `json:"max_value"`
	OwnedBy   string `json:"owned_by,omitempty"`
}

// Extension is an installed extension.
type Extension struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Schema  string `json:"schema"`
}

// Table finds a relation by "name" or "schema.name". Unqualified names are
// resolved in the order of the snapshot's schemas.
func (s *Snapshot) Table(name string) *Table {
	schema, rel := splitQualified(name)
	if schema != "" {
		for _, t := range s.Tables {
			if t.Schema == schema && t.Name == rel {
				return t
			}
		}
		return nil
	}
	for _, sch := range s.Schemas {
		for _, t := range s.Tables {
			if t.Schema == sch && t.Name == rel {
				return t
			}
		}
	}
	return nil
}

// Suggest returns table names similar to name, for "did you mean".
func (s *Snapshot) Suggest(name string) []string {
	_, rel := splitQualified(name)
	var out []string
	for _, t := range s.Tables {
		if strings.Contains(t.Name, rel) || strings.Contains(rel, t.Name) || textutil.Levenshtein(t.Name, rel) <= 2 {
			out = append(out, t.DisplayName())
		}
	}
	return out
}

// BaseTables returns ordinary and partitioned tables (not views, not partitions).
func (s *Snapshot) BaseTables() []*Table {
	var out []*Table
	for _, t := range s.Tables {
		if (t.Kind == KindTable || t.Kind == KindPartitioned) && t.PartitionOf == "" {
			out = append(out, t)
		}
	}
	return out
}

// IndexCount returns the number of indexes across all tables.
func (s *Snapshot) IndexCount() int {
	n := 0
	for _, t := range s.Tables {
		n += len(t.Indexes)
	}
	return n
}

// TotalBytes sums relation sizes.
func (s *Snapshot) TotalBytes() int64 {
	var n int64
	for _, t := range s.Tables {
		n += t.Bytes
	}
	return n
}

func splitQualified(name string) (schema, rel string) {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, '.'); i > 0 {
		return unquote(name[:i]), unquote(name[i+1:])
	}
	return "", unquote(name)
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

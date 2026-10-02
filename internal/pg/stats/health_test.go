package stats

import (
	"testing"

	"github.com/nothikemu/nexus/internal/pg/introspect"
)

func TestHealth(t *testing.T) {
	last := int64(2_000_000_000)
	snap := &introspect.Snapshot{
		Tables: []*introspect.Table{
			{
				Schema: "public", Name: "logs", Kind: introspect.KindTable,
				Stats: introspect.TableStats{DeadTuples: 50000, LiveTuples: 50000},
			},
			{
				Schema: "public", Name: "posts", Kind: introspect.KindTable, PrimaryKey: []string{"id"},
				ForeignKeys: []*introspect.ForeignKey{{Name: "posts_author_fk", Columns: []string{"author_id"}}, {Name: "posts_org_fk", Columns: []string{"org_id"}}},
				Indexes: []*introspect.Index{
					{Name: "posts_pkey", Columns: []string{"id"}, Primary: true, Unique: true, Valid: true, Method: "btree"},
					{Name: "posts_id_idx", Columns: []string{"id"}, Valid: true, Method: "btree"},
					{Name: "posts_org_idx", Columns: []string{"org_id", "created_at"}, Valid: true, Method: "btree"},
					{Name: "posts_broken", Columns: []string{"title"}, Valid: false, Method: "btree"},
				},
			},
		},
		Sequences: []*introspect.Sequence{{Schema: "public", Name: "logs_id_seq", DataType: "integer", LastValue: &last, MaxValue: 2147483647}},
	}
	codes := map[string]Finding{}
	for _, f := range Health(snap) {
		codes[f.Code] = f
	}
	for _, want := range []string{"no_primary_key", "unindexed_foreign_key", "duplicate_index", "invalid_index", "dead_tuples", "sequence_exhaustion"} {
		if _, ok := codes[want]; !ok {
			t.Errorf("missing finding %s (got %v)", want, codes)
		}
	}
	if f := codes["unindexed_foreign_key"]; len(f.Objects) != 1 || f.Objects[0] != "posts(author_id)" {
		t.Errorf("unindexed fks = %v (org_id is covered by a composite index)", f.Objects)
	}
	if f := codes["duplicate_index"]; len(f.Fix) != 1 || f.Fix[0] != `drop index concurrently "public"."posts_id_idx";` {
		t.Errorf("duplicate fix = %v", f.Fix)
	}
}

func TestHealthClean(t *testing.T) {
	snap := &introspect.Snapshot{Tables: []*introspect.Table{{Schema: "public", Name: "a", Kind: introspect.KindTable, PrimaryKey: []string{"id"}}}}
	if got := Health(snap); len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}

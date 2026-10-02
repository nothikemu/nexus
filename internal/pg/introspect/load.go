package introspect

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/pg"
)

// Load reads a snapshot of the given schemas. q may be a pool, a connection
// or a transaction (migration previews snapshot inside an uncommitted
// transaction). Objects owned by extensions are excluded.
func Load(ctx context.Context, q pg.Querier, schemas []string) (*Snapshot, error) {
	s := &Snapshot{Schemas: schemas}
	byName := map[string]*Table{}

	steps := []struct {
		what string
		fn   func() error
	}{
		{"tables", func() error { return loadTables(ctx, q, s, byName) }},
		{"columns", func() error { return loadColumns(ctx, q, schemas, byName) }},
		{"constraints", func() error { return loadConstraints(ctx, q, schemas, byName) }},
		{"indexes", func() error { return loadIndexes(ctx, q, schemas, byName) }},
		{"policies", func() error { return loadPolicies(ctx, q, schemas, byName) }},
		{"triggers", func() error { return loadTriggers(ctx, q, schemas, byName) }},
		{"functions", func() error { return loadFunctions(ctx, q, s) }},
		{"enums", func() error { return loadEnums(ctx, q, s) }},
		{"sequences", func() error { return loadSequences(ctx, q, s) }},
		{"extensions", func() error { return loadExtensions(ctx, q, s) }},
	}
	for _, st := range steps {
		if err := st.fn(); err != nil {
			return nil, fmt.Errorf("reading %s: %w", st.what, err)
		}
	}

	// Reverse relationships.
	for _, t := range s.Tables {
		for _, fk := range t.ForeignKeys {
			if ref := byName[fk.RefQualified()]; ref != nil {
				ref.ReferencedBy = append(ref.ReferencedBy, fk)
			}
		}
	}
	return s, nil
}

// Schemas lists the user-visible schemas in the database.
func Schemas(ctx context.Context, q pg.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `
		select nspname from pg_namespace
		where nspname !~ '^pg_' and nspname <> 'information_schema'
		order by nspname`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

const tablesSQL = `
select n.nspname, c.relname, c.relkind::text,
       coalesce(obj_description(c.oid, 'pg_class'), ''),
       c.relrowsecurity, c.relforcerowsecurity,
       case when coalesce(s.n_live_tup, 0) > 0 or c.reltuples < 0
            then coalesce(s.n_live_tup, 0) else c.reltuples::bigint end,
       case when c.relkind in ('r', 'm', 't') then pg_total_relation_size(c.oid)
            when c.relkind = 'p' then coalesce((
              select sum(pg_total_relation_size(p.relid))::bigint
              from pg_partition_tree(c.oid) p where p.isleaf), 0)
            else 0 end,
       case when c.relkind in ('v', 'm') then pg_get_viewdef(c.oid, true) else '' end,
       coalesce((
         select pn.nspname || '.' || pc.relname
         from pg_inherits i
         join pg_class pc on pc.oid = i.inhparent
         join pg_namespace pn on pn.oid = pc.relnamespace
         where i.inhrelid = c.oid and c.relispartition
         limit 1), ''),
       coalesce(s.seq_scan, 0), coalesce(s.idx_scan, 0),
       coalesce(s.n_dead_tup, 0), coalesce(s.n_live_tup, 0),
       coalesce(s.last_vacuum, s.last_autovacuum) is not null,
       coalesce(s.last_analyze, s.last_autoanalyze) is not null
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
left join pg_stat_user_tables s on s.relid = c.oid
where n.nspname = any($1)
  and c.relkind in ('r', 'p', 'v', 'm', 'f')
  and not exists (select 1 from pg_depend d where d.classid = 'pg_class'::regclass
                  and d.objid = c.oid and d.deptype = 'e')
order by n.nspname, c.relname`

func loadTables(ctx context.Context, q pg.Querier, s *Snapshot, byName map[string]*Table) error {
	rows, err := q.Query(ctx, tablesSQL, s.Schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		t := &Table{}
		var kind string
		if err := rows.Scan(&t.Schema, &t.Name, &kind, &t.Comment, &t.RLSEnabled, &t.RLSForced,
			&t.Rows, &t.Bytes, &t.Definition, &t.PartitionOf,
			&t.Stats.SeqScans, &t.Stats.IdxScans, &t.Stats.DeadTuples, &t.Stats.LiveTuples,
			&t.Stats.HasVacuumed, &t.Stats.HasAnalyzed); err != nil {
			return err
		}
		t.Kind = relkind(kind)
		s.Tables = append(s.Tables, t)
		byName[t.QualifiedName()] = t
	}
	return rows.Err()
}

func relkind(k string) string {
	switch k {
	case "p":
		return KindPartitioned
	case "v":
		return KindView
	case "m":
		return KindMatView
	case "f":
		return KindForeign
	default:
		return KindTable
	}
}

const columnsSQL = `
select n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
       not a.attnotnull,
       case when a.attgenerated = '' then coalesce(pg_get_expr(d.adbin, d.adrelid), '') else '' end,
       case when a.attgenerated <> '' then coalesce(pg_get_expr(d.adbin, d.adrelid), '') else '' end,
       case a.attidentity when 'a' then 'always' when 'd' then 'by default' else '' end,
       coalesce(col_description(c.oid, a.attnum), ''), a.attnum,
       t.typtype = 'e'
from pg_attribute a
join pg_class c on c.oid = a.attrelid
join pg_namespace n on n.oid = c.relnamespace
join pg_type t on t.oid = a.atttypid
left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
where n.nspname = any($1)
  and c.relkind in ('r', 'p', 'v', 'm', 'f')
  and a.attnum > 0 and not a.attisdropped
order by n.nspname, c.relname, a.attnum`

func loadColumns(ctx context.Context, q pg.Querier, schemas []string, byName map[string]*Table) error {
	rows, err := q.Query(ctx, columnsSQL, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table string
		c := &Column{}
		if err := rows.Scan(&schema, &table, &c.Name, &c.Type, &c.Nullable, &c.Default,
			&c.Generated, &c.Identity, &c.Comment, &c.Position, &c.IsEnum); err != nil {
			return err
		}
		if t := byName[schema+"."+table]; t != nil {
			t.Columns = append(t.Columns, c)
		}
	}
	return rows.Err()
}

const constraintsSQL = `
select n.nspname, c.relname, con.conname, con.contype::text,
       pg_get_constraintdef(con.oid, true),
       array(select a.attname::text from unnest(con.conkey) with ordinality k(attnum, ord)
             join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.attnum
             order by k.ord),
       coalesce(fn.nspname, ''), coalesce(fc.relname, ''),
       array(select a.attname::text from unnest(con.confkey) with ordinality k(attnum, ord)
             join pg_attribute a on a.attrelid = con.confrelid and a.attnum = k.attnum
             order by k.ord),
       con.confdeltype::text, con.confupdtype::text
from pg_constraint con
join pg_class c on c.oid = con.conrelid
join pg_namespace n on n.oid = c.relnamespace
left join pg_class fc on fc.oid = con.confrelid
left join pg_namespace fn on fn.oid = fc.relnamespace
where n.nspname = any($1) and con.contype in ('p', 'u', 'f', 'c', 'x')
order by n.nspname, c.relname, con.conname`

func loadConstraints(ctx context.Context, q pg.Querier, schemas []string, byName map[string]*Table) error {
	rows, err := q.Query(ctx, constraintsSQL, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table, name, typ, def, refSchema, refTable, onDel, onUpd string
		var cols, refCols []string
		if err := rows.Scan(&schema, &table, &name, &typ, &def, &cols, &refSchema, &refTable, &refCols, &onDel, &onUpd); err != nil {
			return err
		}
		t := byName[schema+"."+table]
		if t == nil {
			continue
		}
		switch typ {
		case "p":
			t.PrimaryKey = cols
			t.Constraints = append(t.Constraints, &Constraint{Name: name, Type: "primary key", Definition: def, Columns: cols})
		case "f":
			t.ForeignKeys = append(t.ForeignKeys, &ForeignKey{
				Name: name, Schema: schema, Table: table, Columns: cols,
				RefSchema: refSchema, RefTable: refTable, RefColumns: refCols,
				OnDelete: fkAction(onDel), OnUpdate: fkAction(onUpd), Definition: def,
			})
		default:
			t.Constraints = append(t.Constraints, &Constraint{Name: name, Type: contype(typ), Definition: def, Columns: cols})
		}
	}
	return rows.Err()
}

func contype(t string) string {
	switch t {
	case "u":
		return "unique"
	case "x":
		return "exclusion"
	default:
		return "check"
	}
}

func fkAction(a string) string {
	switch a {
	case "c":
		return "cascade"
	case "n":
		return "set null"
	case "d":
		return "set default"
	case "r":
		return "restrict"
	default:
		return "no action"
	}
}

const indexesSQL = `
select n.nspname, t.relname, i.relname, ix.indisunique, ix.indisprimary, am.amname,
       pg_get_indexdef(ix.indexrelid),
       coalesce(pg_get_expr(ix.indpred, ix.indrelid), ''),
       array(select pg_get_indexdef(ix.indexrelid, k, true)
             from generate_series(1, ix.indnkeyatts) k order by k),
       pg_relation_size(i.oid), coalesce(s.idx_scan, 0), ix.indisvalid
from pg_index ix
join pg_class i on i.oid = ix.indexrelid
join pg_class t on t.oid = ix.indrelid
join pg_namespace n on n.oid = t.relnamespace
join pg_am am on am.oid = i.relam
left join pg_stat_user_indexes s on s.indexrelid = ix.indexrelid
where n.nspname = any($1)
order by n.nspname, t.relname, i.relname`

func loadIndexes(ctx context.Context, q pg.Querier, schemas []string, byName map[string]*Table) error {
	rows, err := q.Query(ctx, indexesSQL, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table string
		ix := &Index{}
		if err := rows.Scan(&schema, &table, &ix.Name, &ix.Unique, &ix.Primary, &ix.Method,
			&ix.Definition, &ix.Predicate, &ix.Columns, &ix.Bytes, &ix.Scans, &ix.Valid); err != nil {
			return err
		}
		if t := byName[schema+"."+table]; t != nil {
			t.Indexes = append(t.Indexes, ix)
		}
	}
	return rows.Err()
}

const policiesSQL = `
select n.nspname, c.relname, p.polname, p.polcmd::text, p.polpermissive,
       array(select case when r = 0 then 'public' else pg_get_userbyid(r)::text end
             from unnest(p.polroles) r),
       coalesce(pg_get_expr(p.polqual, p.polrelid), ''),
       coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '')
from pg_policy p
join pg_class c on c.oid = p.polrelid
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = any($1)
order by n.nspname, c.relname, p.polname`

func loadPolicies(ctx context.Context, q pg.Querier, schemas []string, byName map[string]*Table) error {
	rows, err := q.Query(ctx, policiesSQL, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table, cmd string
		p := &Policy{}
		if err := rows.Scan(&schema, &table, &p.Name, &cmd, &p.Permissive, &p.Roles, &p.Using, &p.Check); err != nil {
			return err
		}
		p.Command = map[string]string{"r": "SELECT", "a": "INSERT", "w": "UPDATE", "d": "DELETE"}[cmd]
		if p.Command == "" {
			p.Command = "ALL"
		}
		if t := byName[schema+"."+table]; t != nil {
			t.Policies = append(t.Policies, p)
		}
	}
	return rows.Err()
}

const triggersSQL = `
select n.nspname, c.relname, t.tgname, pg_get_triggerdef(t.oid, true), t.tgenabled <> 'D'
from pg_trigger t
join pg_class c on c.oid = t.tgrelid
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = any($1) and not t.tgisinternal
order by n.nspname, c.relname, t.tgname`

func loadTriggers(ctx context.Context, q pg.Querier, schemas []string, byName map[string]*Table) error {
	rows, err := q.Query(ctx, triggersSQL, schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table string
		tr := &Trigger{}
		if err := rows.Scan(&schema, &table, &tr.Name, &tr.Definition, &tr.Enabled); err != nil {
			return err
		}
		if t := byName[schema+"."+table]; t != nil {
			t.Triggers = append(t.Triggers, tr)
		}
	}
	return rows.Err()
}

const functionsSQL = `
select n.nspname, p.proname, pg_get_function_identity_arguments(p.oid),
       coalesce(pg_get_function_result(p.oid), ''), l.lanname, p.prokind::text,
       p.provolatile::text, p.prosecdef, md5(coalesce(p.prosrc, '') || coalesce(p.probin, ''))
from pg_proc p
join pg_namespace n on n.oid = p.pronamespace
join pg_language l on l.oid = p.prolang
where n.nspname = any($1)
  and not exists (select 1 from pg_depend d where d.classid = 'pg_proc'::regclass
                  and d.objid = p.oid and d.deptype = 'e')
order by n.nspname, p.proname, 3`

func loadFunctions(ctx context.Context, q pg.Querier, s *Snapshot) error {
	rows, err := q.Query(ctx, functionsSQL, s.Schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		f := &Function{}
		var kind, vol string
		if err := rows.Scan(&f.Schema, &f.Name, &f.Arguments, &f.Returns, &f.Language, &kind, &vol, &f.SecurityDefiner, &f.BodyHash); err != nil {
			return err
		}
		f.Kind = map[string]string{"f": "function", "p": "procedure", "a": "aggregate", "w": "window"}[kind]
		f.Volatility = map[string]string{"i": "immutable", "s": "stable", "v": "volatile"}[vol]
		s.Functions = append(s.Functions, f)
	}
	return rows.Err()
}

const enumsSQL = `
select n.nspname, t.typname,
       array(select e.enumlabel::text from pg_enum e where e.enumtypid = t.oid order by e.enumsortorder)
from pg_type t
join pg_namespace n on n.oid = t.typnamespace
where t.typtype = 'e' and n.nspname = any($1)
  and not exists (select 1 from pg_depend d where d.classid = 'pg_type'::regclass
                  and d.objid = t.oid and d.deptype = 'e')
order by n.nspname, t.typname`

func loadEnums(ctx context.Context, q pg.Querier, s *Snapshot) error {
	rows, err := q.Query(ctx, enumsSQL, s.Schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		e := &Enum{}
		if err := rows.Scan(&e.Schema, &e.Name, &e.Values); err != nil {
			return err
		}
		s.Enums = append(s.Enums, e)
	}
	return rows.Err()
}

const sequencesSQL = `
select s.schemaname::text, s.sequencename::text, s.data_type::text, s.last_value, s.max_value,
       coalesce((
         select dn.nspname || '.' || dc.relname || '.' || a.attname
         from pg_depend d
         join pg_class dc on dc.oid = d.refobjid
         join pg_namespace dn on dn.oid = dc.relnamespace
         join pg_attribute a on a.attrelid = d.refobjid and a.attnum = d.refobjsubid
         where d.classid = 'pg_class'::regclass
           and d.objid = format('%I.%I', s.schemaname, s.sequencename)::regclass
           and d.deptype in ('a', 'i')
         limit 1), '')
from pg_sequences s
where s.schemaname = any($1)
order by 1, 2`

func loadSequences(ctx context.Context, q pg.Querier, s *Snapshot) error {
	rows, err := q.Query(ctx, sequencesSQL, s.Schemas)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		seq := &Sequence{}
		if err := rows.Scan(&seq.Schema, &seq.Name, &seq.DataType, &seq.LastValue, &seq.MaxValue, &seq.OwnedBy); err != nil {
			return err
		}
		s.Sequences = append(s.Sequences, seq)
	}
	return rows.Err()
}

func loadExtensions(ctx context.Context, q pg.Querier, s *Snapshot) error {
	rows, err := q.Query(ctx, `
		select e.extname, e.extversion, n.nspname
		from pg_extension e join pg_namespace n on n.oid = e.extnamespace
		order by e.extname`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		e := &Extension{}
		if err := rows.Scan(&e.Name, &e.Version, &e.Schema); err != nil {
			return err
		}
		s.Extensions = append(s.Extensions, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Slice(s.Extensions, func(i, j int) bool { return s.Extensions[i].Name < s.Extensions[j].Name })
	return nil
}

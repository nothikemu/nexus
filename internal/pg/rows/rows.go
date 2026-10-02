// Package rows pages through a table's rows with search, filters and
// ordering. Every read runs in a read-only transaction, so a filter typed by
// a user can never modify data.
package rows

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
)

// Query builds a paged, filtered SELECT for a table. Identifiers are
// quoted; the search term is a bound parameter; the filter is the caller's
// SQL and runs inside a read-only transaction.
type Query struct {
	Table   *introspect.Table
	Where   string
	Search  string
	OrderBy []OrderKey
	Limit   int
	Offset  int
}

// OrderKey is one sort column.
type OrderKey struct {
	Column string
	Desc   bool
}

// from renders "from <table> where …" and its arguments.
func (q Query) from() (string, []any) {
	var args []any
	from := "from " + pg.QuoteIdent(q.Table.Schema, q.Table.Name)
	var conds []string
	if strings.TrimSpace(q.Where) != "" {
		conds = append(conds, "("+q.Where+")")
	}
	if q.Search != "" {
		args = append(args, "%"+escapeLike(q.Search)+"%")
		var ors []string
		for _, c := range q.Table.Columns {
			ors = append(ors, pg.QuoteIdent(c.Name)+"::text ilike $1")
		}
		conds = append(conds, "("+strings.Join(ors, " or ")+")")
	}
	if len(conds) > 0 {
		from += " where " + strings.Join(conds, " and ")
	}
	return from, args
}

// SQL renders the paged query and its arguments. Without an explicit order,
// rows are ordered by primary key so pages are stable.
func (q Query) SQL() (string, []any) {
	from, args := q.from()
	sql := "select * " + from
	order := q.OrderBy
	if len(order) == 0 {
		for _, c := range q.Table.PrimaryKey {
			order = append(order, OrderKey{Column: c})
		}
	}
	if len(order) > 0 {
		parts := make([]string, len(order))
		for i, k := range order {
			parts[i] = pg.QuoteIdent(k.Column)
			if k.Desc {
				parts[i] += " desc"
			}
		}
		sql += " order by " + strings.Join(parts, ", ")
	}
	return sql + fmt.Sprintf(" limit %d offset %d", q.Limit, q.Offset), args
}

// CountSQL counts matching rows, stopping at limit so huge tables stay fast.
func (q Query) CountSQL(limit int) (string, []any) {
	from, args := q.from()
	return fmt.Sprintf("select count(*) from (select 1 %s limit %d) s", from, limit), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ParseOrder validates "col [asc|desc], …" against the table's columns.
func ParseOrder(tb *introspect.Table, spec string) ([]OrderKey, error) {
	var keys []OrderKey
	for _, part := range strings.Split(spec, ",") {
		f := strings.Fields(part)
		if len(f) == 0 {
			continue
		}
		col := strings.Trim(f[0], `"`)
		if tb.Column(col) == nil {
			return nil, fmt.Errorf("%s has no column %s", tb.DisplayName(), col)
		}
		k := OrderKey{Column: col}
		if len(f) > 1 {
			switch strings.ToLower(f[1]) {
			case "desc":
				k.Desc = true
			case "asc":
			default:
				return nil, fmt.Errorf("sort direction must be asc or desc")
			}
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// CountCap is how far Fetch counts before the caller should report "100,000+".
const CountCap = 100000

// Fetch runs a Query in a read-only transaction and counts matches (up to CountCap+1).
func Fetch(ctx context.Context, q pg.Querier, rq Query) (*pg.Result, int64, error) {
	tx, err := beginReadOnly(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	sql, args := rq.SQL()
	res, err := pg.Query(ctx, tx, sql, 0, args...)
	if err != nil {
		return nil, 0, err
	}
	countSQL, cargs := rq.CountSQL(CountCap + 1)
	var total int64
	if err := tx.QueryRow(ctx, countSQL, cargs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return res, total, nil
}

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

func beginReadOnly(ctx context.Context, q pg.Querier) (pgx.Tx, error) {
	b, ok := q.(txBeginner)
	if !ok {
		return nil, fmt.Errorf("querier can't begin transactions")
	}
	tx, err := b.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "set transaction read only"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

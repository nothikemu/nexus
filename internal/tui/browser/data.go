package browser

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/pg/introspect"
	"github.com/nothikemu/nexus/internal/pg/rows"
)

type loadedMsg struct {
	seq     int
	res     *pg.Result
	total   int64
	err     error
	elapsed time.Duration
}

type savedMsg struct {
	err error
}

// load fetches the current page of the top view.
func load(ctx context.Context, pool *pgxpool.Pool, v *view, limit, seq int) tea.Cmd {
	q := rows.Query{Table: v.table, Where: v.where, Search: v.search, OrderBy: v.order, Limit: limit, Offset: v.offset}
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		start := time.Now()
		res, total, err := rows.Fetch(c, pool, q)
		return loadedMsg{seq: seq, res: res, total: total, err: err, elapsed: time.Since(start)}
	}
}

// edit is a pending single-cell update.
type edit struct {
	column *introspect.Column
	value  *string // nil = NULL
	pk     map[string]pg.Cell
}

// updateSQL builds the parameterised UPDATE for an edit. Values travel as
// text and are cast to the column's type, exactly as PostgreSQL would parse
// them from psql.
func updateSQL(t *introspect.Table, e *edit) (string, []any) {
	var args []any
	set := pg.QuoteIdent(e.column.Name) + " = NULL"
	if e.value != nil {
		args = append(args, *e.value)
		set = fmt.Sprintf("%s = $1::text::%s", pg.QuoteIdent(e.column.Name), e.column.Type)
	}
	var where []string
	for _, col := range t.PrimaryKey {
		c := t.Column(col)
		args = append(args, e.pk[col].Text)
		where = append(where, fmt.Sprintf("%s = $%d::text::%s", pg.QuoteIdent(col), len(args), c.Type))
	}
	return fmt.Sprintf("update %s set %s where %s", pg.QuoteIdent(t.Schema, t.Name), set, strings.Join(where, " and ")), args
}

// save applies an edit in a transaction, refusing to touch more than one row.
func save(ctx context.Context, pool *pgxpool.Pool, t *introspect.Table, e *edit) tea.Cmd {
	sql, args := updateSQL(t, e)
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		err := pgx.BeginFunc(c, pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(c, sql, args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("expected to update 1 row, matched %d — nothing was saved", tag.RowsAffected())
			}
			return nil
		})
		return savedMsg{err: err}
	}
}

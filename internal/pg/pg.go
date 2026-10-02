// Package pg is Nexus's PostgreSQL access layer: connections with sane
// defaults, text-format result sets for faithful display, script execution
// and error classification.
package pg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppName is the application_name Nexus sessions identify as, so Nexus can
// leave its own connections out of activity views.
const AppName = "nexus"

// Querier is satisfied by *pgxpool.Pool, *pgxpool.Conn, *pgx.Conn and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Open creates a connection pool and verifies connectivity.
func Open(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("invalid database url: %w", redact(err))
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, ok := cfg.ConnConfig.RuntimeParams["application_name"]; !ok {
		cfg.ConnConfig.RuntimeParams["application_name"] = AppName
	}
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnConfig.ConnectTimeout+time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Target is a display-safe description of a connection string.
type Target struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
}

// Describe parses a connection string into a Target (never including secrets).
func Describe(connString string) Target {
	cfg, err := pgconn.ParseConfig(connString)
	if err != nil {
		return Target{}
	}
	return Target{Host: cfg.Host, Port: cfg.Port, Database: cfg.Database, User: cfg.User}
}

func (t Target) String() string {
	if t.Host == "" {
		return "(unknown)"
	}
	return fmt.Sprintf("%s@%s:%d/%s", t.User, t.Host, t.Port, t.Database)
}

// Redact masks the password in a URL-style connection string.
func Redact(connString string) string {
	u, err := url.Parse(connString)
	if err != nil || u.User == nil {
		return connString
	}
	if _, has := u.User.Password(); !has {
		return connString
	}
	// u.User.String() is the escaped "user:password" exactly as it appears.
	masked := url.PathEscape(u.User.Username()) + ":•••••"
	return strings.Replace(connString, u.User.String()+"@", masked+"@", 1)
}

func redact(err error) error {
	// pgconn parse errors may echo the connection string; keep only the reason.
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.Contains(msg, "://") {
		msg = msg[i+2:]
	}
	return errors.New(msg)
}

// Column describes a result column.
type Column struct {
	Name string `json:"name"`
	OID  uint32 `json:"oid"`
	Type string `json:"type"`
}

// Cell is a value in PostgreSQL's text representation.
type Cell struct {
	Text string
	Null bool
}

// Result is a text-format result set: values exactly as PostgreSQL prints them.
type Result struct {
	Columns   []Column
	Rows      [][]Cell
	Command   string        // command tag, e.g. "SELECT 3", "INSERT 0 1"
	Affected  int64         // rows affected/returned per the command tag
	Truncated bool          // more rows were returned than kept
	Total     int64         // rows returned by the server
	Duration  time.Duration // server round-trip time
	SQL       string        // the statement, when known
}

// HasRows reports whether the statement returns a row set.
func (r *Result) HasRows() bool { return len(r.Columns) > 0 }

var typeMap = pgtype.NewMap()

// TypeName returns a readable type name for a type OID.
func TypeName(oid uint32) string {
	if t, ok := typeMap.TypeForOID(oid); ok {
		return t.Name
	}
	return fmt.Sprintf("oid:%d", oid)
}

func columnsFrom(fds []pgconn.FieldDescription) []Column {
	cols := make([]Column, len(fds))
	for i, fd := range fds {
		cols[i] = Column{Name: fd.Name, OID: fd.DataTypeOID, Type: TypeName(fd.DataTypeOID)}
	}
	return cols
}

func copyRow(values [][]byte) []Cell {
	row := make([]Cell, len(values))
	for i, v := range values {
		if v == nil {
			row[i] = Cell{Null: true}
		} else {
			row[i] = Cell{Text: string(v)}
		}
	}
	return row
}

// Query runs a parameterised statement and returns text-format results,
// keeping at most limit rows (0 = unlimited).
func Query(ctx context.Context, q Querier, sql string, limit int, args ...any) (*Result, error) {
	start := time.Now()
	rows, err := q.Query(ctx, sql, append([]any{pgx.QueryResultFormats{pgx.TextFormatCode}}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &Result{Columns: columnsFrom(rows.FieldDescriptions()), SQL: sql}
	for rows.Next() {
		res.Total++
		if limit > 0 && len(res.Rows) >= limit {
			res.Truncated = true
			continue
		}
		res.Rows = append(res.Rows, copyRow(rows.RawValues()))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tag := rows.CommandTag()
	res.Command = tag.String()
	res.Affected = tag.RowsAffected()
	res.Duration = time.Since(start)
	return res, nil
}

// ExecScript runs a script with the simple query protocol — exactly like
// psql — returning one Result per statement. Statements run in an implicit
// transaction unless the script manages its own. Each result keeps at most
// limit rows (0 = unlimited). On error, results for statements that completed
// are returned alongside the error.
func ExecScript(ctx context.Context, conn *pgconn.PgConn, script string, limit int) ([]*Result, error) {
	mrr := conn.Exec(ctx, script)
	var results []*Result
	start := time.Now()
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		res := &Result{Columns: columnsFrom(rr.FieldDescriptions())}
		for rr.NextRow() {
			res.Total++
			if limit > 0 && len(res.Rows) >= limit {
				res.Truncated = true
				continue
			}
			res.Rows = append(res.Rows, copyRow(rr.Values()))
		}
		tag, err := rr.Close()
		if err != nil {
			_ = mrr.Close()
			return results, err
		}
		res.Command = tag.String()
		res.Affected = tag.RowsAffected()
		res.Duration = time.Since(start)
		start = time.Now()
		results = append(results, res)
	}
	return results, mrr.Close()
}

// AsPgError unwraps a PostgreSQL server error.
func AsPgError(err error) (*pgconn.PgError, bool) {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

// IsUnreachable reports whether err means the server could not be reached
// (as opposed to the server rejecting a query).
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if pe, ok := AsPgError(err); ok {
		// 57P03: the database system is starting up; 57P01: shutting down.
		return pe.Code == "57P03" || pe.Code == "57P01"
	}
	var ce *pgconn.ConnectError
	if errors.As(err, &ce) {
		var pe *pgconn.PgError
		return !errors.As(ce, &pe)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded)
}

// IsAuthError reports whether the server rejected the credentials.
func IsAuthError(err error) bool {
	pe, ok := AsPgError(err)
	return ok && (pe.Code == "28P01" || pe.Code == "28000")
}

// IsMissingDatabase reports a connection to a database that doesn't exist.
func IsMissingDatabase(err error) bool {
	pe, ok := AsPgError(err)
	return ok && pe.Code == "3D000"
}

// QuoteIdent quotes a possibly schema-qualified identifier.
func QuoteIdent(parts ...string) string {
	return pgx.Identifier(parts).Sanitize()
}

// QuoteLiteral quotes a string literal for use in generated SQL.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ServerVersion returns the server's version string (e.g. "16.4") and number.
func ServerVersion(ctx context.Context, q Querier) (string, int, error) {
	var v string
	var num int
	err := q.QueryRow(ctx, `select current_setting('server_version'), current_setting('server_version_num')::int`).Scan(&v, &num)
	if i := strings.IndexByte(v, ' '); i > 0 {
		v = v[:i]
	}
	return v, num, err
}

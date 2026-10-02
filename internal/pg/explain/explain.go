// Package explain runs EXPLAIN, parses PostgreSQL's JSON plans and turns
// them into plain-language findings.
//
// Every recommendation is grounded in the plan PostgreSQL produced and the
// live catalog: Nexus only suggests an index on a column that exists, and
// only when no usable index already covers it.
package explain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nothikemu/nexus/internal/sqltext"
)

// Node is one node of a query plan. Field names follow EXPLAIN (FORMAT JSON).
type Node struct {
	Type      string `json:"Node Type"`
	Relation  string `json:"Relation Name,omitempty"`
	Schema    string `json:"Schema,omitempty"`
	Alias     string `json:"Alias,omitempty"`
	Index     string `json:"Index Name,omitempty"`
	JoinType  string `json:"Join Type,omitempty"`
	Strategy  string `json:"Strategy,omitempty"`
	Parent    string `json:"Parent Relationship,omitempty"`
	CTE       string `json:"CTE Name,omitempty"`
	Operation string `json:"Operation,omitempty"`

	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`
	PlanRows    float64 `json:"Plan Rows"`
	PlanWidth   int     `json:"Plan Width"`

	ActualStartup float64 `json:"Actual Startup Time"`
	ActualTotal   float64 `json:"Actual Total Time"`
	ActualRows    float64 `json:"Actual Rows"`
	Loops         float64 `json:"Actual Loops"`

	Filter         string  `json:"Filter,omitempty"`
	IndexCond      string  `json:"Index Cond,omitempty"`
	RecheckCond    string  `json:"Recheck Cond,omitempty"`
	HashCond       string  `json:"Hash Cond,omitempty"`
	MergeCond      string  `json:"Merge Cond,omitempty"`
	JoinFilter     string  `json:"Join Filter,omitempty"`
	RemovedFilter  float64 `json:"Rows Removed by Filter"`
	RemovedRecheck float64 `json:"Rows Removed by Index Recheck"`
	RemovedJoin    float64 `json:"Rows Removed by Join Filter"`

	SortKey       []string `json:"Sort Key,omitempty"`
	SortMethod    string   `json:"Sort Method,omitempty"`
	SortSpaceUsed float64  `json:"Sort Space Used"`
	SortSpaceType string   `json:"Sort Space Type,omitempty"`
	HashBatches   float64  `json:"Hash Batches"`
	PeakMemory    float64  `json:"Peak Memory Usage"`

	SharedHit  float64 `json:"Shared Hit Blocks"`
	SharedRead float64 `json:"Shared Read Blocks"`

	Children []*Node `json:"Plans,omitempty"`
}

// Plan is a parsed EXPLAIN result.
type Plan struct {
	Root        *Node   `json:"plan"`
	PlanningMs  float64 `json:"planning_ms"`
	ExecutionMs float64 `json:"execution_ms"`
	Analyzed    bool    `json:"analyzed"`
}

type rawPlan struct {
	Plan     *Node   `json:"Plan"`
	Planning float64 `json:"Planning Time"`
	Exec     float64 `json:"Execution Time"`
}

// Parse decodes EXPLAIN (FORMAT JSON) output.
func Parse(data []byte, analyzed bool) (*Plan, error) {
	var raw []rawPlan
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unexpected EXPLAIN output: %w", err)
	}
	if len(raw) == 0 || raw[0].Plan == nil {
		return nil, errors.New("EXPLAIN returned no plan")
	}
	return &Plan{Root: raw[0].Plan, PlanningMs: raw[0].Planning, ExecutionMs: raw[0].Exec, Analyzed: analyzed}, nil
}

// ErrMultipleStatements means more than one statement was given.
var ErrMultipleStatements = errors.New("explain one statement at a time")

// Run explains sql on conn. With analyze, the statement really executes —
// inside a transaction that is always rolled back, so even INSERT, UPDATE
// and DELETE leave no trace.
func Run(ctx context.Context, conn *pgx.Conn, sql string, analyze bool) (*Plan, error) {
	stmts := sqltext.Split(sql)
	if len(stmts) == 0 {
		return nil, errors.New("nothing to explain")
	}
	if len(stmts) > 1 {
		return nil, ErrMultipleStatements
	}
	stmt := stmts[0].SQL
	opts := "format json"
	if analyze {
		opts = "analyze, buffers, format json"
	}
	query := "explain (" + opts + ") " + stmt

	var data []byte
	if !analyze {
		if err := conn.QueryRow(ctx, query).Scan(&data); err != nil {
			return nil, err
		}
		return Parse(data, false)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := tx.QueryRow(ctx, query).Scan(&data); err != nil {
		return nil, err
	}
	return Parse(data, true)
}

// Walk visits every node depth-first, passing its depth.
func (p *Plan) Walk(fn func(n *Node, depth int)) {
	var walk func(n *Node, d int)
	walk = func(n *Node, d int) {
		fn(n, d)
		for _, c := range n.Children {
			walk(c, d+1)
		}
	}
	if p.Root != nil {
		walk(p.Root, 0)
	}
}

// Label is a one-line description of a node, in psql's wording:
// "Seq Scan on users", "Hash Left Join", "Index Scan using users_pkey on users".
func (n *Node) Label() string {
	label := n.Type
	if n.JoinType != "" && n.JoinType != "Inner" {
		switch {
		case strings.HasSuffix(n.Type, " Join"):
			label = strings.TrimSuffix(n.Type, " Join") + " " + n.JoinType + " Join"
		case n.Type == "Nested Loop":
			label = "Nested Loop " + n.JoinType + " Join"
		}
	}
	if n.Index != "" {
		label += " using " + n.Index
	}
	if n.Relation != "" {
		label += " on " + n.Relation
		if n.Alias != "" && n.Alias != n.Relation {
			label += " " + n.Alias
		}
	}
	if n.CTE != "" {
		label += " on " + n.CTE
	}
	return label
}

// IsScan reports whether the node reads a relation.
func (n *Node) IsScan() bool {
	switch n.Type {
	case "Seq Scan", "Index Scan", "Index Only Scan", "Bitmap Heap Scan", "Tid Scan", "Tid Range Scan":
		return true
	}
	return false
}

func loops(n *Node) float64 {
	if n.Loops <= 0 {
		return 1
	}
	return n.Loops
}

// TotalMs is the node's inclusive time across all loops.
func (n *Node) TotalMs() float64 { return n.ActualTotal * loops(n) }

// SelfMs is the node's exclusive time (inclusive minus children).
func (n *Node) SelfMs() float64 {
	self := n.TotalMs()
	for _, c := range n.Children {
		if c.Parent == "InitPlan" || c.Parent == "SubPlan" {
			continue
		}
		self -= c.TotalMs()
	}
	if self < 0 {
		return 0
	}
	return self
}

// RowsOut is the number of rows the node produced (actual when analyzed).
func (n *Node) RowsOut(analyzed bool) float64 {
	if analyzed {
		return n.ActualRows * loops(n)
	}
	return n.PlanRows
}

// RowsRead estimates the rows a scan node examined.
func (n *Node) RowsRead(analyzed bool) float64 {
	if !analyzed {
		return n.PlanRows
	}
	return (n.ActualRows + n.RemovedFilter + n.RemovedRecheck) * loops(n)
}

// Totals summarises a plan.
type Totals struct {
	RowsScanned  float64 `json:"rows_scanned"`
	RowsReturned float64 `json:"rows_returned"`
	SeqScans     int     `json:"seq_scans"`
	IndexScans   int     `json:"index_scans"`
	TotalCost    float64 `json:"total_cost"`
	SharedHit    float64 `json:"shared_hit_blocks"`
	SharedRead   float64 `json:"shared_read_blocks"`
}

// Totals computes summary figures for the plan.
func (p *Plan) Totals() Totals {
	t := Totals{}
	if p.Root == nil {
		return t
	}
	t.RowsReturned = p.Root.RowsOut(p.Analyzed)
	t.TotalCost = p.Root.TotalCost
	t.SharedHit = p.Root.SharedHit
	t.SharedRead = p.Root.SharedRead
	p.Walk(func(n *Node, _ int) {
		if !n.IsScan() {
			return
		}
		t.RowsScanned += n.RowsRead(p.Analyzed)
		if n.Type == "Seq Scan" {
			t.SeqScans++
		} else {
			t.IndexScans++
		}
	})
	return t
}

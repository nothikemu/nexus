package render

import (
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/ui"
)

// SQLProblem renders a PostgreSQL error as a Problem, with a code frame when
// the source SQL and error position are known. lineOffset shifts frame line
// numbers when source is an excerpt of a file.
func SQLProblem(t *ui.Theme, err error, source string, lineOffset int, position int32) *ui.Problem {
	pe, ok := pg.AsPgError(err)
	if !ok {
		return &ui.Problem{Title: ui.SayFirst(ui.MomentBroke), Detail: err.Error(), Err: err}
	}
	pr := &ui.Problem{Title: pe.Message + ".", Code: pe.Code, Err: err, Detail: pe.Detail, Hint: pe.Hint}
	if source != "" && position > 0 {
		pr.Frame = t.CodeFrame(source, int(position), lineOffset, "")
	} else if pe.Where != "" && pr.Detail == "" {
		pr.Detail = pe.Where
	}
	return pr
}

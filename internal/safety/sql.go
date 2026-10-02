package safety

import (
	"strings"

	"github.com/nothikemu/nexus/internal/sqltext"
)

// ClassifySQL estimates the safety level of a script from the leading
// keywords of its statements. It errs on the side of caution: anything it
// doesn't recognise as read-only is at least a safe write. The reasons list
// the statements that made it destructive.
func ClassifySQL(script string) (Level, []string) {
	level := ReadOnly
	var reasons []string
	for _, st := range sqltext.Split(script) {
		l := classify(st.SQL)
		if l == Destructive {
			reasons = append(reasons, firstLine(st.SQL))
		}
		if l > level {
			level = l
		}
	}
	return level, reasons
}

func classify(stmt string) Level {
	words := strings.Fields(strings.ToLower(sqltext.StripComments(stmt)))
	if len(words) == 0 {
		return ReadOnly
	}
	has := func(w string) bool {
		for _, x := range words {
			if strings.Trim(x, "(),;") == w {
				return true
			}
		}
		return false
	}
	switch words[0] {
	case "select", "show", "values", "table", "explain":
		// SELECT … INTO creates a table; EXPLAIN ANALYZE executes the statement.
		if words[0] == "explain" && has("analyze") {
			return classify(strings.Join(words[2:], " "))
		}
		if words[0] == "select" && has("into") {
			return SafeWrite
		}
		return ReadOnly
	case "with":
		switch {
		case has("delete"), has("truncate"):
			return Destructive
		case has("insert"), has("update"), has("merge"):
			return SafeWrite
		}
		return ReadOnly
	case "drop", "truncate", "delete":
		return Destructive
	case "alter":
		if has("drop") {
			return Destructive
		}
		return SafeWrite
	case "update":
		if !has("where") {
			return Destructive // every row
		}
		return SafeWrite
	default:
		return SafeWrite
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if len(s) > 80 {
		s = s[:79] + "…"
	}
	return s
}

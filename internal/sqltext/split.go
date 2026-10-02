// Package sqltext understands the lexical structure of PostgreSQL scripts:
// where statements end, whether input is complete, and how error positions
// map back to lines and columns.
package sqltext

import (
	"strings"
	"unicode/utf8"
)

// Statement is one statement in a script.
type Statement struct {
	SQL    string // statement text, trimmed, without the trailing semicolon
	Offset int    // byte offset of SQL within the original script
	Line   int    // 1-based line where the statement starts
}

// Split splits a script into statements. It understands single-quoted and
// E” strings, quoted identifiers, dollar-quoted bodies, line and (nested)
// block comments, and SQL-standard BEGIN ATOMIC function bodies. Statements
// containing only whitespace and comments are dropped.
func Split(script string) []Statement {
	var out []Statement
	s := newScanner(script)
	start := 0
	for {
		end, ok := s.nextTerminator()
		if !ok {
			end = len(script)
		}
		if st, keep := makeStatement(script, start, end); keep {
			out = append(out, st)
		}
		if !ok {
			return out
		}
		start = end + 1
		s.resetStatement()
	}
}

// Complete reports whether input forms one or more complete statements:
// it ends (ignoring trailing whitespace and comments) with a semicolon that
// is outside of any string, identifier, comment or function body.
func Complete(input string) bool {
	if strings.TrimSpace(StripComments(input)) == "" {
		return false
	}
	s := newScanner(input)
	last := -1
	for {
		end, ok := s.nextTerminator()
		if !ok {
			break
		}
		last = end
		s.resetStatement()
	}
	if last < 0 || s.inLiteral() {
		return false
	}
	return strings.TrimSpace(StripComments(input[last+1:])) == ""
}

// StripComments removes comments outside of literals.
func StripComments(script string) string {
	var b strings.Builder
	s := newScanner(script)
	for s.pos < len(s.src) {
		from := s.pos
		kind := s.step()
		if kind != tokComment {
			b.WriteString(s.src[from:s.pos])
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// LineCol converts a byte offset into a 1-based line and rune column.
func LineCol(src string, offset int) (line, col int) {
	if offset > len(src) {
		offset = len(src)
	}
	line, col = 1, 1
	for _, r := range src[:offset] {
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// CharToByte converts a 1-based character position (as PostgreSQL reports
// error positions) into a byte offset.
func CharToByte(src string, pos int) int {
	if pos <= 1 {
		return 0
	}
	i := 0
	for n := 1; n < pos && i < len(src); n++ {
		_, size := utf8.DecodeRuneInString(src[i:])
		i += size
	}
	return i
}

func makeStatement(script string, start, end int) (Statement, bool) {
	raw := script[start:end]
	if strings.TrimSpace(StripComments(raw)) == "" {
		return Statement{}, false
	}
	lead := skipTrivia(raw)
	text := strings.TrimSpace(raw[lead:])
	line, _ := LineCol(script, start+lead)
	return Statement{SQL: text, Offset: start + lead, Line: line}, true
}

// skipTrivia returns the length of leading whitespace and comments.
func skipTrivia(s string) int {
	i := 0
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r':
			i++
		case strings.HasPrefix(s[i:], "--") || strings.HasPrefix(s[i:], "/*"):
			sc := newScanner(s[i:])
			sc.step()
			i += sc.pos
		default:
			return i
		}
	}
	return i
}

type tokKind int

const (
	tokOther tokKind = iota
	tokComment
	tokString
	tokWord
	tokSemicolon
)

type scanner struct {
	src string
	pos int

	// unterminated literal at end of input
	open bool

	// statement-level state for BEGIN ATOMIC bodies
	words      int
	isRoutine  bool
	sawCreate  bool
	blockDepth int
}

func newScanner(src string) *scanner { return &scanner{src: src} }

func (s *scanner) resetStatement() {
	s.words, s.isRoutine, s.sawCreate, s.blockDepth = 0, false, false, 0
}

func (s *scanner) inLiteral() bool { return s.open || s.blockDepth > 0 }

// nextTerminator advances to the next statement-ending semicolon and returns
// its byte offset.
func (s *scanner) nextTerminator() (int, bool) {
	for s.pos < len(s.src) {
		from := s.pos
		if s.step() == tokSemicolon && s.blockDepth == 0 {
			return from, true
		}
	}
	return 0, false
}

// step consumes one token and returns its kind.
func (s *scanner) step() tokKind {
	src := s.src
	c := src[s.pos]
	switch {
	case c == '-' && s.peek(1) == '-':
		nl := strings.IndexByte(src[s.pos:], '\n')
		if nl < 0 {
			s.pos = len(src)
		} else {
			s.pos += nl + 1
		}
		return tokComment
	case c == '/' && s.peek(1) == '*':
		depth := 0
		for s.pos < len(src) {
			if src[s.pos] == '/' && s.peek(1) == '*' {
				depth++
				s.pos += 2
			} else if src[s.pos] == '*' && s.peek(1) == '/' {
				depth--
				s.pos += 2
				if depth == 0 {
					return tokComment
				}
			} else {
				s.pos++
			}
		}
		s.open = true
		return tokComment
	case c == '\'':
		s.quoted('\'', s.escapeStringPrefix())
		return tokString
	case c == '"':
		s.quoted('"', false)
		return tokString
	case c == '$':
		if tag, ok := s.dollarTag(); ok {
			s.pos += len(tag)
			idx := strings.Index(src[s.pos:], tag)
			if idx < 0 {
				s.pos = len(src)
				s.open = true
			} else {
				s.pos += idx + len(tag)
			}
			return tokString
		}
		s.pos++
		return tokOther
	case c == ';':
		s.pos++
		return tokSemicolon
	case isIdentStart(c):
		from := s.pos
		for s.pos < len(src) && isIdentChar(src[s.pos]) {
			s.pos++
		}
		s.word(strings.ToLower(src[from:s.pos]))
		return tokWord
	default:
		s.pos++
		return tokOther
	}
}

func (s *scanner) peek(n int) byte {
	if s.pos+n < len(s.src) {
		return s.src[s.pos+n]
	}
	return 0
}

// escapeStringPrefix reports whether the quote at pos opens an E” string.
func (s *scanner) escapeStringPrefix() bool {
	if s.pos == 0 {
		return false
	}
	p := s.src[s.pos-1]
	if p != 'e' && p != 'E' {
		return false
	}
	return s.pos < 2 || !isIdentChar(s.src[s.pos-2])
}

func (s *scanner) quoted(q byte, backslash bool) {
	src := s.src
	s.pos++
	for s.pos < len(src) {
		c := src[s.pos]
		if backslash && c == '\\' {
			s.pos += 2
			continue
		}
		if c == q {
			if s.peek(1) == q { // doubled quote escapes itself
				s.pos += 2
				continue
			}
			s.pos++
			return
		}
		s.pos++
	}
	s.pos = len(src)
	s.open = true
}

// dollarTag returns the $tag$ starting at pos, if any. $1-style parameters
// are not tags.
func (s *scanner) dollarTag() (string, bool) {
	src := s.src
	if s.pos > 0 && isIdentChar(src[s.pos-1]) {
		return "", false // e.g. identifier containing $
	}
	i := s.pos + 1
	if i < len(src) && src[i] >= '0' && src[i] <= '9' {
		return "", false
	}
	for i < len(src) && isIdentChar(src[i]) && src[i] != '$' {
		i++
	}
	if i < len(src) && src[i] == '$' {
		return src[s.pos : i+1], true
	}
	return "", false
}

// word tracks keywords that open and close SQL-standard routine bodies:
// CREATE [OR REPLACE] FUNCTION|PROCEDURE … BEGIN ATOMIC … END.
func (s *scanner) word(w string) {
	s.words++
	if s.words == 1 {
		s.sawCreate = w == "create"
		return
	}
	if s.sawCreate && !s.isRoutine && s.words <= 4 && (w == "function" || w == "procedure") {
		s.isRoutine = true
		return
	}
	if !s.isRoutine {
		return
	}
	switch w {
	case "begin", "case":
		s.blockDepth++
	case "end":
		if s.blockDepth > 0 {
			s.blockDepth--
		}
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

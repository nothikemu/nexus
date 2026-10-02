// Package migrate is Nexus's migration engine: versioned SQL files with up
// and down sections, checksummed history, advisory locking, transactional
// application, previews and repair.
//
// A migration file is named <version>_<name>.sql, where version is a UTC
// timestamp (YYYYMMDDHHMMSS):
//
//	-- nexus:up
//	create table public.profiles (...);
//
//	-- nexus:down
//	drop table public.profiles;
//
// "-- nexus:no-transaction" runs the up section statement by statement
// outside a transaction (for CREATE INDEX CONCURRENTLY and friends).
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nothikemu/nexus/internal/sqltext"
)

// Migration is a parsed migration file.
type Migration struct {
	Version       string `json:"version"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Up            string `json:"-"`
	Down          string `json:"-"`
	UpLine        int    `json:"-"` // 1-based file line where the up SQL starts
	DownLine      int    `json:"-"`
	HasDown       bool   `json:"reversible"`
	Empty         bool   `json:"empty"` // the up section has no SQL yet
	NoTransaction bool   `json:"no_transaction"`
	Checksum      string `json:"checksum"`
}

// ID is "version_name".
func (m *Migration) ID() string { return m.Version + "_" + m.Name }

var (
	fileRe   = regexp.MustCompile(`^(\d{1,20})_([A-Za-z0-9][A-Za-z0-9_-]*)\.sql$`)
	markerRe = regexp.MustCompile(`(?i)^\s*--\s*nexus:\s*(up|down|no-transaction)\s*$`)
)

// ParseError describes a malformed migration file.
type ParseError struct {
	Path string
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

// ParseFile reads and parses a migration file.
func ParseFile(path string) (*Migration, error) {
	base := filepath.Base(path)
	m := fileRe.FindStringSubmatch(base)
	if m == nil {
		return nil, &ParseError{Path: path, Msg: "file name must look like <version>_<name>.sql, e.g. 20261002141500_add_profiles.sql"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mig, err := Parse(string(data))
	if err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			pe.Path = path
		}
		return nil, err
	}
	mig.Version, mig.Name, mig.Path = m[1], m[2], path
	return mig, nil
}

// Parse parses migration source text.
func Parse(src string) (*Migration, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	m := &Migration{}

	type section int
	const (
		header section = iota
		up
		down
	)
	cur := header
	sawUp, sawDown, sawMarker := false, false, false
	var upB, downB, headB []string
	for i, line := range lines {
		mm := markerRe.FindStringSubmatch(line)
		if mm == nil {
			switch cur {
			case header:
				headB = append(headB, line)
			case up:
				upB = append(upB, line)
			case down:
				downB = append(downB, line)
			}
			continue
		}
		sawMarker = true
		switch strings.ToLower(mm[1]) {
		case "up":
			if sawUp {
				return nil, &ParseError{Line: i + 1, Msg: "more than one -- nexus:up marker"}
			}
			if sawDown {
				return nil, &ParseError{Line: i + 1, Msg: "-- nexus:up must come before -- nexus:down"}
			}
			for j, h := range headB {
				if hasSQL(h) {
					return nil, &ParseError{Line: j + 1, Msg: "SQL found before -- nexus:up; move it into the up section"}
				}
			}
			sawUp, cur = true, up
			m.UpLine = i + 2
		case "down":
			if sawDown {
				return nil, &ParseError{Line: i + 1, Msg: "more than one -- nexus:down marker"}
			}
			if !sawUp {
				// Everything before the down marker is the up section.
				upB, headB = headB, nil
				m.UpLine = 1
				sawUp = true
			}
			sawDown, cur = true, down
			m.DownLine = i + 2
		case "no-transaction":
			m.NoTransaction = true
		}
	}
	if !sawMarker || (!sawUp && !sawDown) {
		upB = headB
		m.UpLine = 1
	}
	m.Up = strings.Join(upB, "\n")
	m.Down = strings.Join(downB, "\n")
	m.HasDown = sawDown && hasSQL(m.Down)
	m.Empty = !hasSQL(m.Up)
	if !m.NoTransaction {
		for _, sec := range []struct {
			sql  string
			line int
		}{{m.Up, m.UpLine}, {m.Down, m.DownLine}} {
			for _, st := range sqltext.Split(sec.sql) {
				if isTransactionControl(st.SQL) {
					return nil, &ParseError{Line: sec.line + st.Line - 1, Msg: "transaction control (BEGIN/COMMIT/ROLLBACK) isn't allowed: nexus runs each migration in its own transaction; use -- nexus:no-transaction to manage it yourself"}
				}
			}
		}
	}
	m.Checksum = Checksum(m.Up)
	return m, nil
}

// Checksum fingerprints SQL, ignoring line endings and trailing whitespace
// so that editor normalisation never reads as a change.
func Checksum(sql string) string {
	lines := strings.Split(strings.ReplaceAll(sql, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	norm := strings.Trim(strings.Join(lines, "\n"), "\n")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// isTransactionControl reports whether a statement begins or ends a transaction.
func isTransactionControl(stmt string) bool {
	fields := strings.Fields(strings.ToLower(sqltext.StripComments(stmt)))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "begin", "commit", "rollback", "end", "abort":
		return true
	case "start":
		return len(fields) > 1 && fields[1] == "transaction"
	}
	return false
}

// hasSQL reports whether text contains anything besides whitespace and
// line comments.
func hasSQL(text string) bool {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "--") {
			return true
		}
	}
	return false
}

// LoadDir parses every migration in dir, sorted by version. A missing
// directory yields no migrations.
func LoadDir(dir string) ([]*Migration, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Migration
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m, err := ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if other, dup := seen[m.Version]; dup {
			return nil, &ParseError{Path: m.Path, Msg: fmt.Sprintf("version %s is also used by %s", m.Version, other)}
		}
		seen[m.Version] = filepath.Base(m.Path)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return versionLess(out[i].Version, out[j].Version) })
	return out, nil
}

// versionLess compares numeric versions of possibly different lengths.
func versionLess(a, b string) bool {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

var nameCleanRe = regexp.MustCompile(`[^a-z0-9_]+`)

// SanitizeName turns "Add Profiles!" into "add_profiles".
func SanitizeName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = nameCleanRe.ReplaceAllString(n, "_")
	return strings.Trim(n, "_")
}

// Create writes a new, empty migration and returns its path.
func Create(dir, name string, now time.Time) (string, error) {
	clean := SanitizeName(name)
	if clean == "" {
		return "", fmt.Errorf("migration name %q has no usable characters", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	existing, err := LoadDir(dir)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	var latest string
	for _, m := range existing {
		used[m.Version] = true
		latest = m.Version
	}
	t := now.UTC()
	version := t.Format("20060102150405")
	// Keep versions unique and monotonic even when created within the same
	// second or when the clock is behind the newest migration.
	for used[version] || (latest != "" && !versionLess(latest, version)) {
		t = t.Add(time.Second)
		version = t.Format("20060102150405")
	}
	path := filepath.Join(dir, version+"_"+clean+".sql")
	body := "-- " + strings.ReplaceAll(clean, "_", " ") + "\n\n-- nexus:up\n\n\n-- nexus:down\n\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

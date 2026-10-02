package repl

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

const maxHistory = 1000

// history is the shell's command history, persisted as JSON lines so that
// multi-line statements survive intact.
type history struct {
	path    string
	entries []string
	pos     int    // index while browsing; len(entries) = editing a new line
	draft   string // what was typed before browsing started
}

func loadHistory(path string) *history {
	h := &history{path: path}
	if path != "" {
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			for sc.Scan() {
				var s string
				if json.Unmarshal(sc.Bytes(), &s) == nil && s != "" {
					h.entries = append(h.entries, s)
				}
			}
			f.Close()
		}
	}
	if len(h.entries) > maxHistory {
		h.entries = h.entries[len(h.entries)-maxHistory:]
	}
	h.pos = len(h.entries)
	return h
}

func (h *history) add(s string) {
	h.pos = len(h.entries)
	if n := len(h.entries); n > 0 && h.entries[n-1] == s {
		return
	}
	h.entries = append(h.entries, s)
	h.pos = len(h.entries)
	if h.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(h.path), 0o700)
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(s)
	_, _ = f.Write(append(b, '\n'))
}

// prev moves back in history, remembering the current draft.
func (h *history) prev(current string) (string, bool) {
	if h.pos == 0 {
		return "", false
	}
	if h.pos == len(h.entries) {
		h.draft = current
	}
	h.pos--
	return h.entries[h.pos], true
}

// next moves forward, ending at the saved draft.
func (h *history) next() (string, bool) {
	if h.pos >= len(h.entries) {
		return "", false
	}
	h.pos++
	if h.pos == len(h.entries) {
		return h.draft, true
	}
	return h.entries[h.pos], true
}

func (h *history) last(n int) []string {
	if len(h.entries) <= n {
		return h.entries
	}
	return h.entries[len(h.entries)-n:]
}

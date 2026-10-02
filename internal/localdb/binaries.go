package localdb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Binaries is a PostgreSQL installation found on this machine.
type Binaries struct {
	Dir     string `json:"dir"`
	Version string `json:"version"` // e.g. "16.14"
	Major   int    `json:"major"`
}

// Path returns the full path of a PostgreSQL program in this installation.
func (b Binaries) Path(program string) string {
	return filepath.Join(b.Dir, exe(program))
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

var versionRe = regexp.MustCompile(`\(PostgreSQL\)\s+(\d+)(?:\.(\d+))?`)

// FindBinaries discovers PostgreSQL installations, newest first. It looks at
// NEXUS_PG_BIN, pg_config and pg_ctl on PATH, and the standard install
// locations of apt, yum, Homebrew, Postgres.app and the Windows installer.
func FindBinaries(ctx context.Context) []Binaries {
	var dirs []string
	if d := os.Getenv("NEXUS_PG_BIN"); d != "" {
		dirs = append(dirs, d)
	}
	if pgc, err := exec.LookPath("pg_config"); err == nil {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, err := exec.CommandContext(c, pgc, "--bindir").Output()
		cancel()
		if err == nil {
			dirs = append(dirs, strings.TrimSpace(string(out)))
		}
	}
	if p, err := exec.LookPath("pg_ctl"); err == nil {
		dirs = append(dirs, filepath.Dir(p))
	}
	for _, pattern := range candidateGlobs() {
		matches, _ := filepath.Glob(pattern)
		dirs = append(dirs, matches...)
	}

	seen := map[string]bool{}
	var found []Binaries
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		if b, ok := probe(ctx, abs); ok {
			found = append(found, b)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return versionGreater(found[i].Version, found[j].Version) })
	return found
}

func candidateGlobs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/opt/postgresql@*/bin",
			"/opt/homebrew/opt/postgresql/bin",
			"/usr/local/opt/postgresql@*/bin",
			"/usr/local/opt/postgresql/bin",
			"/Applications/Postgres.app/Contents/Versions/*/bin",
			"/Library/PostgreSQL/*/bin",
		}
	case "windows":
		return []string{`C:\Program Files\PostgreSQL\*\bin`}
	default:
		return []string{
			"/usr/lib/postgresql/*/bin",
			"/usr/pgsql-*/bin",
			"/usr/local/pgsql/bin",
			"/opt/postgresql/*/bin",
		}
	}
}

// probe checks that a directory has the programs Nexus needs and reads the version.
func probe(ctx context.Context, dir string) (Binaries, bool) {
	for _, p := range []string{"initdb", "pg_ctl", "postgres"} {
		if _, err := os.Stat(filepath.Join(dir, exe(p))); err != nil {
			return Binaries{}, false
		}
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, filepath.Join(dir, exe("pg_ctl")), "--version").Output()
	if err != nil {
		return Binaries{}, false
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return Binaries{}, false
	}
	major, _ := strconv.Atoi(m[1])
	v := m[1]
	if m[2] != "" {
		v += "." + m[2]
	}
	return Binaries{Dir: dir, Version: v, Major: major}, true
}

// pick chooses binaries for a major version. When want is 0 any version
// will do. exact reports whether the chosen major matches want.
func pick(all []Binaries, want int) (b Binaries, exact bool, ok bool) {
	for _, x := range all {
		if x.Major == want {
			return x, true, true
		}
	}
	if len(all) > 0 {
		return all[0], want == 0, true
	}
	return Binaries{}, false, false
}

func versionGreater(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

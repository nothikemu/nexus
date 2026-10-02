// Package project discovers Nexus projects on disk, scaffolds new ones, and
// manages per-project local state in .nexus/.
package project

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nothikemu/nexus/internal/config"
)

// StateDirName is the git-ignored directory holding local state.
const StateDirName = ".nexus"

// ErrNotFound means no nexus.yaml exists in the directory or its parents.
var ErrNotFound = errors.New("not inside a nexus project")

// Project is a loaded Nexus project.
type Project struct {
	Root       string
	ConfigPath string
	Config     *config.Config
	Warnings   []string
}

// Find searches start and its parents for nexus.yaml and loads it.
func Find(start string) (*Project, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(dir, config.FileName)
		if _, err := os.Stat(path); err == nil {
			return Load(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

// Load loads the project rooted at dir.
func Load(dir string) (*Project, error) {
	path := filepath.Join(dir, config.FileName)
	cfg, warnings, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return &Project{Root: dir, ConfigPath: path, Config: cfg, Warnings: warnings}, nil
}

// Name is the project name.
func (p *Project) Name() string { return p.Config.Project.Name }

// Path joins elements onto the project root.
func (p *Project) Path(elem ...string) string {
	return filepath.Join(append([]string{p.Root}, elem...)...)
}

// StateDir is the absolute path of .nexus/.
func (p *Project) StateDir() string { return p.Path(StateDirName) }

// MigrationsDir is the absolute migrations directory.
func (p *Project) MigrationsDir() string { return p.Path(p.Config.Migrations.Dir) }

// SeedFiles expands the configured seed globs into a sorted, de-duplicated list.
func (p *Project) SeedFiles() ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, pattern := range p.Config.Seeds.Paths {
		matches, err := filepath.Glob(p.Path(pattern))
		if err != nil {
			return nil, fmt.Errorf("seeds.paths: %w", err)
		}
		sort.Strings(matches)
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// Rel returns path relative to the project root when possible.
func (p *Project) Rel(path string) string {
	if rel, err := filepath.Rel(p.Root, path); err == nil {
		return rel
	}
	return path
}

// LocalState is per-machine state for the local environment. It holds the
// generated database password and is stored with 0600 permissions in
// .nexus/local.json, which is git-ignored.
type LocalState struct {
	Password  string    `json:"password"`
	CreatedAt time.Time `json:"created_at"`
}

// LocalState loads (or creates) the project's local state.
func (p *Project) LocalState() (*LocalState, error) {
	path := filepath.Join(p.StateDir(), "local.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var s LocalState
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s is corrupt: %w", p.Rel(path), err)
		}
		if s.Password != "" {
			return &s, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	pw, err := RandomSecret(32)
	if err != nil {
		return nil, err
	}
	s := &LocalState{Password: pw, CreatedAt: time.Now().UTC()}
	if err := os.MkdirAll(p.StateDir(), 0o700); err != nil {
		return nil, err
	}
	data, _ = json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return s, nil
}

const secretAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomSecret returns n cryptographically random URL-safe characters.
func RandomSecret(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(secretAlphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = secretAlphabet[v.Int64()]
	}
	return string(b), nil
}

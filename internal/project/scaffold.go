package project

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/nothikemu/nexus/internal/config"
)

// ScaffoldOptions controls `nexus init`.
type ScaffoldOptions struct {
	Dir     string         // target directory (created if missing)
	Name    string         // project name
	Blank   bool           // skip the starter migration and seed
	Port    int            // 0 = pick a free port
	Runtime config.Runtime // "" = auto
	Version int            // 0 = default
	Now     time.Time      // migration timestamp (zero = now)
}

// ScaffoldResult reports what was created.
type ScaffoldResult struct {
	Root    string
	Created []string // paths relative to Root
	Updated []string // existing files that were amended (.gitignore)
	Config  *config.Config
}

// ErrExists means the directory already holds a Nexus project.
var ErrExists = errors.New("a nexus project already exists here")

// Scaffold creates a new project.
func Scaffold(opts ScaffoldOptions) (*ScaffoldResult, error) {
	root, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, config.FileName)); err == nil {
		return nil, ErrExists
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	cfg := config.Default(opts.Name)
	if opts.Runtime != "" {
		cfg.Database.Runtime = opts.Runtime
	}
	if opts.Version != 0 {
		cfg.Database.Version = opts.Version
	}
	cfg.Database.Port = opts.Port
	if cfg.Database.Port == 0 {
		cfg.Database.Port = FreePort(config.DefaultPort, config.DefaultPort+80)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	res := &ScaffoldResult{Root: root, Config: cfg}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	yamlBody, err := render(configTemplate, cfg)
	if err != nil {
		return nil, err
	}
	files := []struct {
		rel  string
		body string
	}{
		{config.FileName, yamlBody},
	}
	if !opts.Blank {
		version := opts.Now.UTC().Format("20060102150405")
		mig, err := render(starterMigration, cfg)
		if err != nil {
			return nil, err
		}
		seed, err := render(starterSeed, cfg)
		if err != nil {
			return nil, err
		}
		files = append(files,
			struct{ rel, body string }{filepath.Join(cfg.Migrations.Dir, version+"_init.sql"), mig},
			struct{ rel, body string }{filepath.Join("seeds", "seed.sql"), seed},
		)
	}
	for _, f := range files {
		path := filepath.Join(root, f.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if _, err := os.Stat(path); err == nil {
			continue // never overwrite user files
		}
		if err := os.WriteFile(path, []byte(f.body), 0o644); err != nil {
			return nil, err
		}
		res.Created = append(res.Created, f.rel)
	}
	for _, dir := range []string{cfg.Migrations.Dir, "seeds"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return nil, err
		}
	}

	created, err := ensureGitignore(root)
	if err != nil {
		return nil, err
	}
	if created {
		res.Created = append(res.Created, ".gitignore")
	} else {
		res.Updated = append(res.Updated, ".gitignore")
	}
	return res, nil
}

// ensureGitignore makes sure .nexus/ (which holds local credentials) is
// ignored. It returns true when the file was newly created.
func ensureGitignore(root string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, os.WriteFile(path, []byte("# Nexus local state: database files and generated credentials.\n"+StateDirName+"/\n"), 0o644)
	}
	if err != nil {
		return false, err
	}
	if IgnoresStateDir(string(data)) {
		return false, nil
	}
	sep := ""
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		sep = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n# Nexus local state: database files and generated credentials.\n%s/\n", sep, StateDirName)
	return false, err
}

// IgnoresStateDir reports whether .gitignore content ignores .nexus/.
func IgnoresStateDir(gitignore string) bool {
	for _, line := range strings.Split(gitignore, "\n") {
		switch strings.TrimSpace(line) {
		case StateDirName, StateDirName + "/", "/" + StateDirName, "/" + StateDirName + "/":
			return true
		}
	}
	return false
}

// FreePort returns the first port in [from, to) accepting a listener on
// 127.0.0.1, or from if none is free.
func FreePort(from, to int) int {
	for p := from; p < to; p++ {
		if PortFree(p) {
			return p
		}
	}
	return from
}

// PortFree reports whether 127.0.0.1:port can be bound.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func render(tpl string, cfg *config.Config) (string, error) {
	t, err := template.New("").Parse(tpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, cfg); err != nil {
		return "", err
	}
	return b.String(), nil
}

const configTemplate = `# nexus.yaml — configuration for {{.Project.Name}}
# Docs: https://github.com/nothikemu/nexus/blob/main/docs/CONFIG.md

project:
  name: {{.Project.Name}}

database:
  version: {{.Database.Version}}          # PostgreSQL major used for local development
  runtime: {{.Database.Runtime}}        # auto | native | docker | external
  port: {{.Database.Port}}
  name: {{.Database.Name}}
  schemas: [public]    # schemas Nexus inspects and manages
  # url: ${DATABASE_URL}   # connect to an existing database (runtime: external)

migrations:
  dir: {{.Migrations.Dir}}

seeds:
  paths: [seeds/*.sql]

dev:
  auto_migrate: true   # apply pending migrations on ` + "`nexus dev`" + `
  auto_seed: true      # seed a freshly created local database

# Remote environments. Values are read from your shell environment at the
# moment you target them (--env staging); one environment's secrets are never
# used for another.
#
# environments:
#   staging:
#     database_url: ${NEXUS_STAGING_DATABASE_URL}
#   production:
#     database_url: ${NEXUS_PRODUCTION_DATABASE_URL}
#     protected: true
`

const starterMigration = `-- Your first migration. Edit it, or add more with:
--   nexus migration create <name>
--
-- Everything between "nexus:up" and "nexus:down" runs in one transaction.

-- nexus:up
create table public.users (
  id          uuid        primary key default gen_random_uuid(),
  email       text        not null unique,
  name        text,
  created_at  timestamptz not null default now()
);

comment on table public.users is 'People who use {{.Project.Name}}.';

-- nexus:down
drop table public.users;
`

const starterSeed = `-- Seed data for local development. Runs on a fresh database with ` + "`nexus dev`" + `,
-- or any time with ` + "`nexus db seed`" + `. Keep it idempotent.

insert into public.users (email, name) values
  ('ada@example.com',    'Ada Lovelace'),
  ('grace@example.com',  'Grace Hopper'),
  ('edsger@example.com', 'Edsger Dijkstra')
on conflict (email) do nothing;
`

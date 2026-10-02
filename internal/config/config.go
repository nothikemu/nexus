// Package config defines nexus.yaml: its schema, defaults, validation,
// environment interpolation and diagnostics.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the project configuration file name.
const FileName = "nexus.yaml"

// Runtime selects how the local database runs.
type Runtime string

// Supported local runtimes.
const (
	RuntimeAuto     Runtime = "auto"
	RuntimeNative   Runtime = "native"
	RuntimeDocker   Runtime = "docker"
	RuntimeExternal Runtime = "external"
)

// Supported PostgreSQL majors for local development.
const (
	MinPostgresVersion     = 13
	MaxPostgresVersion     = 18
	DefaultPostgresVersion = 16
	DefaultPort            = 54320
)

// Config is the parsed nexus.yaml.
type Config struct {
	Project      Project                `yaml:"project" json:"project"`
	Database     Database               `yaml:"database" json:"database"`
	Migrations   Migrations             `yaml:"migrations" json:"migrations"`
	Seeds        Seeds                  `yaml:"seeds" json:"seeds"`
	Dev          Dev                    `yaml:"dev" json:"dev"`
	Environments map[string]Environment `yaml:"environments" json:"environments,omitempty"`
}

// Project identifies the project.
type Project struct {
	Name string `yaml:"name" json:"name"`
}

// Database configures the local development database.
type Database struct {
	Version int      `yaml:"version" json:"version"`
	Runtime Runtime  `yaml:"runtime" json:"runtime"`
	Port    int      `yaml:"port" json:"port"`
	Name    string   `yaml:"name" json:"name"`
	Schemas []string `yaml:"schemas" json:"schemas"`
	// URL connects to an existing database (runtime: external). May contain
	// ${VAR} references.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Image overrides the Docker image (runtime: docker).
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
}

// Migrations configures the migration system.
type Migrations struct {
	Dir string `yaml:"dir" json:"dir"`
}

// Seeds configures seed data.
type Seeds struct {
	Paths []string `yaml:"paths" json:"paths"`
}

// Dev configures `nexus dev`.
type Dev struct {
	AutoMigrate *bool `yaml:"auto_migrate,omitempty" json:"auto_migrate"`
	AutoSeed    *bool `yaml:"auto_seed,omitempty" json:"auto_seed"`
}

// Environment is a named remote target (staging, production, …).
type Environment struct {
	DatabaseURL string `yaml:"database_url" json:"database_url,omitempty"`
	// Protected environments require a typed confirmation for destructive
	// operations. Defaults to true for environments named production/prod.
	Protected *bool `yaml:"protected,omitempty" json:"protected,omitempty"`
}

// ShouldAutoMigrate reports whether `nexus dev` applies pending migrations.
func (d Dev) ShouldAutoMigrate() bool { return d.AutoMigrate == nil || *d.AutoMigrate }

// ShouldAutoSeed reports whether `nexus dev` seeds a freshly created database.
func (d Dev) ShouldAutoSeed() bool { return d.AutoSeed == nil || *d.AutoSeed }

// IsProtected reports whether an environment requires typed confirmation.
func (e Environment) IsProtected(name string) bool {
	if e.Protected != nil {
		return *e.Protected
	}
	n := strings.ToLower(name)
	return n == "production" || n == "prod"
}

// Default returns a configuration with defaults for a project name.
func Default(name string) *Config {
	c := &Config{Project: Project{Name: name}}
	c.applyDefaults()
	return c
}

func (c *Config) applyDefaults() {
	if c.Database.Version == 0 {
		c.Database.Version = DefaultPostgresVersion
	}
	if c.Database.Runtime == "" {
		c.Database.Runtime = RuntimeAuto
		if c.Database.URL != "" {
			c.Database.Runtime = RuntimeExternal
		}
	}
	if c.Database.Port == 0 {
		c.Database.Port = DefaultPort
	}
	if c.Database.Name == "" {
		c.Database.Name = DatabaseName(c.Project.Name)
	}
	if len(c.Database.Schemas) == 0 {
		c.Database.Schemas = []string{"public"}
	}
	if c.Migrations.Dir == "" {
		c.Migrations.Dir = "migrations"
	}
	if c.Seeds.Paths == nil {
		c.Seeds.Paths = []string{"seeds/*.sql"}
	}
}

// Load reads and validates a config file. Warnings (unknown keys) are
// returned alongside a valid config; errors make the config unusable.
func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data)
}

// Parse decodes, defaults and validates configuration bytes.
func Parse(data []byte) (*Config, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, &Error{Problems: []string{yamlMessage(err)}}
	}
	var c Config
	if len(bytes.TrimSpace(data)) > 0 {
		if err := root.Decode(&c); err != nil {
			return nil, nil, &Error{Problems: []string{yamlMessage(err)}}
		}
	}
	warnings := unknownKeys(&root)
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, warnings, err
	}
	return &c, warnings, nil
}

// Error collects validation problems.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	if len(e.Problems) == 1 {
		return "invalid " + FileName + ": " + e.Problems[0]
	}
	return fmt.Sprintf("invalid %s: %d problems: %s", FileName, len(e.Problems), strings.Join(e.Problems, "; "))
}

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	identRe   = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]{0,62}$`)
	envNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

// Validate checks semantic rules.
func (c *Config) Validate() error {
	var p []string
	if c.Project.Name == "" {
		p = append(p, "project.name is required")
	} else if !nameRe.MatchString(c.Project.Name) {
		p = append(p, fmt.Sprintf("project.name %q must be lowercase letters, digits, '-' or '_'", c.Project.Name))
	}
	switch c.Database.Runtime {
	case RuntimeAuto, RuntimeNative, RuntimeDocker:
	case RuntimeExternal:
		if c.Database.URL == "" {
			p = append(p, "database.url is required when database.runtime is external")
		}
	default:
		p = append(p, fmt.Sprintf("database.runtime %q must be one of auto, native, docker, external", c.Database.Runtime))
	}
	if v := c.Database.Version; v < MinPostgresVersion || v > MaxPostgresVersion {
		p = append(p, fmt.Sprintf("database.version %d is not supported (use %d–%d)", v, MinPostgresVersion, MaxPostgresVersion))
	}
	if port := c.Database.Port; port < 1024 || port > 65535 {
		p = append(p, fmt.Sprintf("database.port %d must be between 1024 and 65535", port))
	}
	if !identRe.MatchString(c.Database.Name) {
		p = append(p, fmt.Sprintf("database.name %q is not a valid PostgreSQL identifier", c.Database.Name))
	}
	for _, s := range c.Database.Schemas {
		if !identRe.MatchString(s) {
			p = append(p, fmt.Sprintf("database.schemas: %q is not a valid schema name", s))
		}
	}
	if filepath.IsAbs(c.Migrations.Dir) || strings.Contains(filepath.ToSlash(c.Migrations.Dir), "..") {
		p = append(p, "migrations.dir must be a relative path inside the project")
	}
	for name, env := range c.Environments {
		if name == "local" {
			p = append(p, "environments.local is reserved: the local environment is configured by the database section")
			continue
		}
		if !envNameRe.MatchString(name) {
			p = append(p, fmt.Sprintf("environments: %q must be lowercase letters, digits, '-' or '_'", name))
		}
		if env.DatabaseURL == "" {
			p = append(p, fmt.Sprintf("environments.%s.database_url is required", name))
		}
	}
	if len(p) > 0 {
		return &Error{Problems: p}
	}
	return nil
}

// DatabaseName derives a PostgreSQL database name from a project name.
func DatabaseName(project string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(project) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "db_" + name
	}
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// EnvironmentNames lists configured remote environments, sorted.
func (c *Config) EnvironmentNames() []string {
	names := make([]string, 0, len(c.Environments))
	for n := range c.Environments {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var errNoSuchEnv = errors.New("no such environment")

// Environment returns a named remote environment.
func (c *Config) Environment(name string) (Environment, error) {
	env, ok := c.Environments[name]
	if !ok {
		return Environment{}, fmt.Errorf("%w %q", errNoSuchEnv, name)
	}
	return env, nil
}

func yamlMessage(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "yaml: ")
	return strings.ReplaceAll(msg, "unmarshal errors:\n  ", "")
}

package localdb

import (
	"context"
	"io"
	"time"

	"github.com/nothikemu/nexus/internal/config"
	"github.com/nothikemu/nexus/internal/pg"
	"github.com/nothikemu/nexus/internal/project"
)

// External is a database Nexus connects to but never manages.
type External struct {
	proj *project.Project
}

// Kind implements Runtime.
func (e *External) Kind() config.Runtime { return config.RuntimeExternal }

// URL implements Runtime.
func (e *External) URL() (string, error) {
	return config.Interpolate("database.url", e.proj.Config.Database.URL, nil)
}

// Check implements Runtime.
func (e *External) Check(context.Context) error {
	_, err := e.URL()
	return err
}

// Status implements Runtime.
func (e *External) Status(ctx context.Context) (*Status, error) {
	st := &Status{Runtime: config.RuntimeExternal, Initialized: true}
	url, err := e.URL()
	if err != nil {
		return st, err
	}
	t := pg.Describe(url)
	st.Location, st.Port = t.Host, int(t.Port)
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pg.Open(c, url)
	if err != nil {
		return st, nil
	}
	defer pool.Close()
	st.Running = true
	st.Version, _, _ = pg.ServerVersion(c, pool)
	return st, nil
}

// Start implements Runtime.
func (e *External) Start(context.Context) (*StartResult, error) { return nil, ErrExternal }

// Stop implements Runtime.
func (e *External) Stop(context.Context) error { return ErrExternal }

// Destroy implements Runtime.
func (e *External) Destroy(context.Context) error { return ErrExternal }

// Logs implements Runtime.
func (e *External) Logs(context.Context, io.Writer, int, bool) error { return ErrExternal }

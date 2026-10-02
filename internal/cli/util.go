package cli

import (
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nothikemu/nexus/internal/pg"
)

func asPg(err error) (*pgconn.PgError, bool) { return pg.AsPgError(err) }

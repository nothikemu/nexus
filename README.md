<p align="center">
  <img src="docs/assets/mascot.png" alt="The Nexus core and its ten expressions" width="720">
</p>

<h1 align="center">Nexus</h1>

<p align="center"><b>your database, right in the terminal.</b><br>
A PostgreSQL-native backend platform with a little personality.</p>

---

Nexus is one CLI for your whole backend. Underneath, it is deliberately
serious: real PostgreSQL, real transactions, checksummed migrations,
catalog-accurate introspection, typed confirmations before anything
destructive. On the surface it is meant to be a pleasure to use — colour,
typography, a live dashboard, and the **Nexus core**, a small glowing
data-being that lives in your terminal and reacts to what's going on.

```bash
nexus init my-app
cd my-app
nexus dev
```

<p align="center"><img src="docs/assets/dev.png" alt="nexus dev starting PostgreSQL, applying migrations and seeding" width="760"></p>

> **Status: Phase 1 of 6.** This release is the core: the CLI, the visual
> system and mascot, local PostgreSQL, migrations, schema inspection, the SQL
> shell, the table explorer, the query analyzer and the dashboard. Auth, REST,
> storage, realtime, functions, jobs and the rest are designed in
> [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and arrive in later phases.
> Nexus never shows a service it doesn't actually run.

## Install

Nexus is a single Go binary. With Go 1.24+:

```bash
go install github.com/nothikemu/nexus/cmd/nexus@latest
```

or from a checkout: `make build` → `./bin/nexus`.

For the local database Nexus uses PostgreSQL installed on your machine
(`brew install postgresql@16`, `apt install postgresql`, Postgres.app, …),
or Docker, or any existing database you point it at.

## What you get today

| | |
|---|---|
| **`nexus init`** | A project: `nexus.yaml`, a first migration, seed data, and git-ignore rules that keep local credentials local. |
| **`nexus dev`** | Starts a per-project PostgreSQL in the background (native `initdb`/`pg_ctl`, or Docker), applies pending migrations, seeds a fresh database, and shows what's running. |
| **`nexus`** | A live dashboard: database health, throughput sparklines from real `pg_stat_database` deltas, migrations, activity, and the table explorer one keypress away. If the database is asleep, press `w` to wake it. |
| **`nexus sql`** | Run SQL from arguments, files or pipes — or an interactive shell with multi-line input, history, cancellation (`ctrl+c` cancels the query, not the shell), `\d`, `\explain` and friends. |
| **`nexus table users browse`** | Page, search, filter with SQL, sort, inspect records and JSON, follow foreign keys, and edit cells by primary key. Filters run in read-only transactions. |
| **`nexus migration …`** | `create`, `status`, `diff` (runs pending migrations in a transaction, reports the schema diff, rolls back), `apply`, `rollback`, `repair`. Checksums catch edited history; an advisory lock stops concurrent deploys. |
| **`nexus query analyze`** | Runs `EXPLAIN ANALYZE` in a rolled-back transaction, draws where the time goes, and suggests fixes — only for columns and indexes that really exist. |
| **`nexus db …`** | `inspect` (health checks: missing primary keys, unindexed foreign keys, duplicate or invalid indexes, bloat, sequence exhaustion), `tables`, `schema`, `explain`, `vacuum`, `analyze`, `seed`, `shell`, `url`. |
| **`nexus status` / `doctor`** | What's running and whether your setup is healthy — config, PostgreSQL, Docker, connection, migrations, schema, secrets, terminal. |

<table>
<tr>
<td><img src="docs/assets/dashboard.png" alt="The nexus dashboard"></td>
<td><img src="docs/assets/table-browser.png" alt="The table browser"></td>
</tr>
<tr>
<td><img src="docs/assets/sql-shell.png" alt="The SQL shell"></td>
<td><img src="docs/assets/migrate-and-analyze.png" alt="Previewing and applying a migration, then analyzing a slow query"></td>
</tr>
</table>

### Errors that tell you what to do

Every failure explains what happened, why, and what to try next. SQL errors
point at the exact character — in migration files, with the real file line.

<p align="center"><img src="docs/assets/migration-error.png" alt="A migration error with a code frame" width="680"></p>

## Safety

Every operation has a safety level — **READ ONLY**, **SAFE WRITE** or
**DESTRUCTIVE** — and Nexus never performs a destructive one silently.

- Destructive operations show exactly what will happen and ask first.
- Against a **protected environment** (production by default) they require a
  typed phrase — `DELETE PRODUCTION DATA` — and `--yes` is never enough. In
  CI, pass the phrase with `--confirm`.
- Secrets are resolved only for the environment you target; a missing
  production secret fails loudly instead of falling back to anything.
- Local database passwords are generated per project, stored with `0600`
  permissions in the git-ignored `.nexus/` directory, and the server listens
  on `127.0.0.1` only.

## Works everywhere

Nexus adapts to where it runs: truecolor → 256 → 16 colours automatically, no
blocking terminal queries (safe in tmux and over SSH), and animation only on
interactive terminals.

| flag | effect |
|---|---|
| `--json` | machine-readable output on stdout; errors as JSON on stderr |
| `--plain` | no colour, no animation, ASCII only — for CI logs and screen readers |
| `--no-color` | honours `NO_COLOR` too |
| `--no-animation` | static output, same content |

Exit codes: `0` ok · `1` failed · `2` usage · `3` declined or needs confirmation · `4` database unreachable · `130` interrupted.

## Explore any database

You don't need a project to use Nexus as a database tool:

```bash
nexus --db-url "$DATABASE_URL" db inspect
nexus --db-url "$DATABASE_URL" table orders browse
NEXUS_DATABASE_URL=postgres://… nexus sql
```

## Documentation

- [docs/CLI.md](docs/CLI.md) — every command and flag
- [docs/CONFIG.md](docs/CONFIG.md) — `nexus.yaml`, environments and secrets
- [docs/DESIGN.md](docs/DESIGN.md) — the visual language, the mascot, and the voice
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how it's built, and the plan for phases 2–6

## Development

```bash
make build        # ./bin/nexus with version info
make lint         # gofmt, go vet, staticcheck
make test         # unit tests; integration tests skip without a database

# integration tests run against a real PostgreSQL (13–18)
export NEXUS_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres
make integration
```

Each integration test gets its own throwaway database. CI runs the suite
against PostgreSQL 13, 16 and 17 and builds on Linux, macOS and Windows.

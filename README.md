<p align="center">
  <img src="docs/assets/mascot.png" alt="Nex, the Nexus mascot, in all twelve moods" width="760">
</p>

<h1 align="center">Nexus</h1>

<p align="center"><b>your database, right in the terminal.</b><br>
A friendly PostgreSQL toolkit — with a tiny companion named Nex.</p>

<p align="center">
  <a href="docs/INSTALL.md">Install</a> ·
  <a href="docs/GETTING-STARTED.md">Getting started</a> ·
  <a href="docs/README.md">Docs</a> ·
  <a href="docs/MEET-NEX.md">Meet Nex</a>
</p>

---

Nexus runs a PostgreSQL database for your project and gives you everything
around it in one command: migrations, a SQL shell, a table explorer, a
query analyzer, health checks and a live dashboard.

Underneath it's serious — real PostgreSQL, real transactions, checksummed
migrations, and confirmations before anything destructive. On the surface
it's meant to be a pleasure: colour, clear errors, and **Nex**, a little
round creature who lives in your terminal and keeps an eye on things.

```bash
nexus init my-app      # create a project — and start it, if you like
nexus                  # the live dashboard
```

<p align="center"><img src="docs/assets/hi-and-pet.png" alt="Nex saying hello and being patted" width="640"></p>

## Install

**macOS, Linux, WSL:**

```bash
curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | sh
```

**With Go 1.24+:**

```bash
go install github.com/nothikemu/nexus/cmd/nexus@latest
```

Nexus also needs **PostgreSQL** (`brew install postgresql@16`,
`sudo apt install postgresql`, Postgres.app…) **or Docker** to run your
local database — or you can point it at a database you already have.
Then run `nexus doctor` to check everything.

→ Full details, Windows, shell completion and uninstalling:
**[docs/INSTALL.md](docs/INSTALL.md)**

## Use it

### 1. Start a project

```bash
nexus init blog
```

Nex wakes up, creates `blog/` with a config file, a first migration and some
sample data, then asks whether to start the database. Say yes.

<p align="center"><img src="docs/assets/dev.png" alt="nexus up: PostgreSQL started, migrations applied, data seeded" width="720"></p>

### 2. Look around

```bash
cd blog
nexus                  # dashboard — press 2 for tables, enter to browse
nexus tables           # list tables
nexus table users      # structure of one table
nexus browse users     # explore rows: search, filter, sort, edit
nexus sql              # SQL shell (or: nexus sql "select * from users")
```

### 3. Change the schema

```bash
nexus migration create add_posts    # write the SQL in the new file
nexus migration diff                # preview — nothing is saved
nexus migration apply               # apply it
nexus migration rollback            # undo it
```

### 4. Make things fast

```bash
nexus query analyze "select * from posts where title = 'hello'"
nexus db inspect
```

Nexus shows where the time goes and suggests fixes — only for columns and
indexes that really exist.

### 5. Day to day

```bash
nexus up / nexus down   # start / stop the local database (data stays)
nexus status            # what's running
nexus doctor            # is everything OK?
nexus guide             # every command, on one screen
nexus hi                # say hi to Nex (plus a daily tip)
```

New to it all? The **[getting started guide](docs/GETTING-STARTED.md)** walks
through a whole project in about five minutes.

<table>
<tr>
<td><img src="docs/assets/dashboard.png" alt="The nexus dashboard"></td>
<td><img src="docs/assets/table-browser.png" alt="The table explorer"></td>
</tr>
<tr>
<td><img src="docs/assets/sql-shell.png" alt="The SQL shell"></td>
<td><img src="docs/assets/migrate-and-analyze.png" alt="Previewing and applying a migration, then analyzing a query"></td>
</tr>
</table>

## Meet Nex

Nex is round, pastel, mostly calm and occasionally delighted. Nex shows how
things are going: asleep when your database is off, curious when something
needs a look, celebrating when everything connects — and hearts when you
`nexus pet`. Nex blinks, glances around, waves hello, and has a greeting
for every time of day.

Prefer a quiet tool? `--plain` turns Nex (and colour and animation) off.
**[Meet Nex →](docs/MEET-NEX.md)**

## Errors that help

Every error says what happened, why, and what to do next. SQL errors point
at the exact character — in migration files, with the real line number.

<p align="center"><img src="docs/assets/migration-error.png" alt="A migration error pointing at the exact typo" width="640"></p>

## Safe by default

- Anything destructive shows what will happen and asks first.
- Production is **protected**: destructive commands need you to type
  `DELETE PRODUCTION DATA` — `--yes` alone is never enough.
- Migrations run in transactions: if one fails, nothing half-applies.
- Query analysis and table filters can't change your data.
- Local passwords are generated per project, kept out of git, and the
  database only listens on `127.0.0.1`.

## Works everywhere

Rich colour in modern terminals, sensible fallbacks everywhere else, and
modes for scripts:

| flag | |
|---|---|
| `--json` | machine-readable output |
| `--plain` | no colour, no mascot, no animation (CI, screen readers) |
| `--no-color` / `--no-animation` | just those |

Use it without a project, against any database:
`nexus --db-url "$DATABASE_URL" browse orders`.

## Documentation

| | |
|---|---|
| [Install](docs/INSTALL.md) | every way to install, plus PostgreSQL/Docker |
| [Getting started](docs/GETTING-STARTED.md) | a five-minute tutorial |
| [Guides](docs/README.md#guides) | migrations · SQL shell · table explorer · dashboard · performance · environments & CI |
| [Commands](docs/CLI.md) | every command and flag |
| [Configuration](docs/CONFIG.md) | `nexus.yaml` |
| [Troubleshooting](docs/TROUBLESHOOTING.md) · [FAQ](docs/FAQ.md) | when something's off |
| [Architecture](docs/ARCHITECTURE.md) · [Design](docs/DESIGN.md) | how it's built and the roadmap |

> **Status:** this is Phase 1 of 6 — the database core and the terminal
> experience. Auth, APIs, storage, realtime, functions and jobs are designed
> in [ARCHITECTURE.md](docs/ARCHITECTURE.md) and come next. Nexus never shows
> a service it doesn't actually run.

## Contributing

```bash
make build        # ./bin/nexus
make lint         # gofmt, go vet, staticcheck
make test         # unit tests

# integration tests need a PostgreSQL to create throwaway databases in
export NEXUS_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres
make integration
```

Releases: `git tag v0.x.y && git push --tags` builds binaries for Linux,
macOS and Windows (amd64 + arm64) with checksums, which the install script
uses.

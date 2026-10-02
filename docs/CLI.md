# Command reference

Every command has `--help`. Every command that produces data supports
`--json`. This page is checked against the real command tree by
`internal/cli/docs_test.go`.

## Global flags

| flag | |
|---|---|
| `--json` | machine-readable output on stdout; errors as JSON on stderr |
| `--plain` | no colour, no animation, ASCII only |
| `--no-color` | disable colour (`NO_COLOR` works too) |
| `--no-animation` | disable spinners and animation |
| `-e, --env <name>` | target environment (default `local`, or `NEXUS_ENV`) |
| `--db-url <url>` | use this database instead (or `NEXUS_DATABASE_URL`) |
| `-C, --project <dir>` | run as if started in this directory |
| `-y, --yes` | skip confirmations — never enough for protected environments |
| `--confirm <phrase>` | typed confirmation for protected environments, for CI |
| `-v, --verbose` | structured diagnostics on stderr |

## Start

### `nexus`

Opens the live dashboard on an interactive terminal; prints `nexus status`
otherwise. Keys: `1`–`4` switch views, `↑↓` select, `enter` browses a table,
`w` wakes a sleeping local database, `r` refreshes, `q` quits.

### `nexus init [name]`

Creates a project (in `name`, or the current directory).

| flag | |
|---|---|
| `--blank` | skip the starter migration and seed |
| `--runtime auto\|native\|docker` | local database runtime |
| `--port <n>` | local port (default: first free from 54320) |
| `--pg-version <n>` | PostgreSQL major (default 16) |

### `nexus dev`

Starts the local database in the background, applies pending migrations
(`dev.auto_migrate`), seeds a fresh database (`dev.auto_seed`) and shows a
status panel.

### `nexus dev status`

The local database process: state, runtime and why it was chosen, version,
port, PID, data location.

### `nexus dev stop`

Stops the local database. Data is kept.

### `nexus dev reset`

**Destructive.** Deletes the local database and recreates it: fresh cluster,
all migrations, seeds.

### `nexus dev logs`

Server logs, coloured by severity. `-f` follows, `-n <lines>` sets how many.

### `nexus status`

Project, environment, database, schema size, migrations, health and
activity. Being offline is a state, not an error.

### `nexus doctor`

Checks configuration, PostgreSQL binaries, Docker, runtime resolution,
connectivity, migrations, schema health, local secrets and the terminal.
Exits 1 if any check fails.

## Database

### `nexus sql [query]`

Runs statements one at a time (each autocommits, like psql) and prints each
result. Input comes from the arguments, `-f file.sql`, or stdin. With none
of these on an interactive terminal it opens the SQL shell.

| flag | |
|---|---|
| `-f, --file <path>` | run a file; errors show file line numbers |
| `-x, --expanded` | one record per block |
| `--limit <n>` | rows kept per result (default 1000, 0 = all) |

Against a protected environment, statements that write ask for confirmation
and destructive ones (`DROP`, `TRUNCATE`, `DELETE`, `UPDATE` without `WHERE`,
`ALTER … DROP`) require the typed phrase.

**The SQL shell.** `enter` runs once a statement ends with `;`
(`alt+enter` adds a line), `↑↓` walk history (kept per project in
`.nexus/sql_history`), `ctrl+c` cancels a running query or clears the line,
`ctrl+d` quits. Meta commands: `\dt`, `\d [name]`, `\dn`, `\explain <q>`,
`\analyze <q>`, `\x`, `\timing`, `\conninfo`, `\history`, `\?`, `\q`.

### `nexus db`

Same as `nexus db inspect`.

### `nexus db shell`

Opens `psql` connected to the target when it is installed, otherwise the Nexus
SQL shell. `--builtin` forces the Nexus shell.

### `nexus db inspect`

Size, objects, connections, cache hit ratio, transactions, uptime,
extensions — then health findings with suggested fixes: tables without
primary keys, foreign keys without indexes, duplicate and invalid indexes,
tables that need vacuuming, sequences close to exhaustion, large unused
indexes.

### `nexus db tables`

Tables with rows, size, columns and notes. `--all` adds views, materialized
views and partitions.

### `nexus db schema [table]`

The structure as a tree: schemas → tables, views, functions, enums,
sequences, plus extensions. `--columns` includes every column.

### `nexus db explain <sql>`

The planner's estimated plan, drawn as a tree with each node's share of the
cost. `--analyze` runs the query — inside a transaction that is always rolled
back — and shows real timings, rows and recommendations.

### `nexus db vacuum [table]`

VACUUM a table or the whole database, reporting size before and after.
`--analyze` (default on) refreshes statistics; `--full` rewrites the table
(it is locked meanwhile, so Nexus asks first).

### `nexus db analyze [table]`

Refresh planner statistics.

### `nexus db seed`

Run the seed files (`seeds.paths`), each in its own transaction.

### `nexus db url`

Print the connection string — `export DATABASE_URL=$(nexus db url)`.
`--redacted` masks the password.

### `nexus table <name> [view]`

| view | |
|---|---|
| *(none)* | headline, columns with attributes, indexes, relationships |
| `columns` | columns only |
| `rows` | a page of rows |
| `browse` | the interactive explorer |
| `indexes` | indexes with size and scan counts |
| `relations` | foreign keys in both directions |
| `policies` | row-level security status and policies |

`rows` flags: `-n, --limit`, `--offset`, `-w, --where <sql>`,
`-o, --order "col desc, …"`, `-s, --search <text>`, `-x, --expanded`.
Every read runs in a read-only transaction, so a filter can never modify
data.

**The explorer** (`browse`): `↑↓←→` move, `enter` inspects a record (JSON
pretty-printed), `/` searches every column, `f` filters with SQL, `s` cycles
sort on the selected column, `n`/`p` page, `g` follows a foreign key, `esc`
goes back, `e` edits the selected cell (by primary key, one row, in a
transaction; `ctrl+n` sets NULL). Editing is disabled on protected
environments, for generated and identity-always columns, and on tables
without a primary key.

### `nexus migration`

Same as `nexus migration status`. Aliases: `migrations`, `migrate`.

### `nexus migration create <name>`

Creates `migrations/<UTC timestamp>_<name>.sql` with `-- nexus:up` and
`-- nexus:down` sections.

### `nexus migration status`

Every migration: applied (when, how long), pending, out of order, edited
after being applied, or missing its file.

### `nexus migration diff`

Runs pending migrations inside a transaction, compares the schema before and
after, reports the differences and rolls back. Nothing is changed.

### `nexus migration apply`

Applies pending migrations, each in its own transaction, under an advisory
lock. Refuses when history has drifted or pending migrations are out of
order.

| flag | |
|---|---|
| `--dry-run` | same as `diff` |
| `--to <version>` | stop after this version |
| `--allow-out-of-order` | apply migrations older than the latest applied one (after merging branches) |

### `nexus migration rollback`

**Destructive.** Runs the down sections of the most recently applied
migrations, newest first. `--steps <n>` (default 1).

### `nexus migration repair`

Reconciles history with the files: accepts the checksum of edited
migrations, forgets history for deleted files. Never touches your schema.

### `nexus query`

Query performance tools.

### `nexus query analyze <sql>`

Same as `nexus db explain --analyze`.

### `nexus query explain <sql>`

Same as `nexus db explain`.

## More

### `nexus mascot [state]`

Meet the Nexus core. With a state (`idle`, `thinking`, `working`,
`connecting`, `success`, `celebrating`, `curious`, `warning`, `error`,
`sleeping`) it shows that expression; `--loop` animates until interrupted.

### `nexus version`

Version, commit, build date, Go version and platform.

### `nexus completion <shell>`

Shell completion scripts for bash, zsh, fish and PowerShell. Table names
complete from the live database.

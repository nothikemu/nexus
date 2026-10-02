# nexus.yaml

Every Nexus project has a `nexus.yaml` at its root. Nexus finds it by
searching the current directory and its parents (or the directory given with
`-C`). `nexus init` writes a commented one for you.

```yaml
project:
  name: my-app

database:
  version: 16          # PostgreSQL major for local development (13–18)
  runtime: auto        # auto | native | docker | external
  port: 54320          # local port, bound to 127.0.0.1 only
  name: my_app         # database name
  schemas: [public]    # schemas Nexus inspects and manages
  # url: ${DATABASE_URL}     # runtime: external — use an existing server
  # image: postgres:16-alpine  # runtime: docker — override the image

migrations:
  dir: migrations

seeds:
  paths: [seeds/*.sql]

dev:
  auto_migrate: true   # nexus dev applies pending migrations
  auto_seed: true      # nexus dev seeds a freshly created database

environments:
  staging:
    database_url: ${NEXUS_STAGING_DATABASE_URL}
  production:
    database_url: ${NEXUS_PRODUCTION_DATABASE_URL}
    protected: true
```

## project

| key | default | |
|---|---|---|
| `name` | required | lowercase letters, digits, `-` and `_` |

## database

| key | default | |
|---|---|---|
| `version` | `16` | PostgreSQL major used for local development. An existing local cluster always keeps the major it was created with. |
| `runtime` | `auto` | `native` runs PostgreSQL from your machine's binaries in `.nexus/postgres`. `docker` runs a `postgres:<version>` container with a named volume. `external` connects to `url` and never starts, stops or deletes anything. `auto` picks native when PostgreSQL is installed (and you aren't root), else Docker. |
| `port` | first free from `54320` | local port, bound to `127.0.0.1` only |
| `name` | derived from the project name | database name |
| `schemas` | `[public]` | schemas used by introspection, health checks, diffs and the explorer |
| `url` | — | connection string for `runtime: external`; may reference `${VARS}` |
| `image` | `postgres:<version>` | Docker image override |

## migrations

| key | default | |
|---|---|---|
| `dir` | `migrations` | relative to the project root |

Migration files are named `<version>_<name>.sql` where version is a UTC
timestamp. `nexus migration create <name>` makes one:

```sql
-- nexus:up
create table public.profiles (
  user_id uuid primary key references public.users (id) on delete cascade,
  bio     text
);

-- nexus:down
drop table public.profiles;
```

- Everything in the up section runs in **one transaction**. If any statement
  fails, the whole migration is rolled back and nothing after it runs.
- Transaction control (`BEGIN`, `COMMIT`, …) isn't allowed in transactional
  migrations. For statements that can't run in a transaction, such as
  `CREATE INDEX CONCURRENTLY`, add `-- nexus:no-transaction`; statements then
  run one at a time.
- Nexus stores a SHA-256 checksum of each applied up section. Editing an
  applied migration is detected; `nexus migration repair` accepts the edit
  once you're sure.
- History lives in the `nexus_meta` schema (`nexus_meta.migrations`,
  `nexus_meta.migration_events`). It is deliberately not called `nexus`:
  PostgreSQL's default `search_path` starts with `"$user"`, so a schema named
  like a plausible role would silently capture unqualified tables.

## seeds

| key | default | |
|---|---|---|
| `paths` | `[seeds/*.sql]` | globs, run in sorted order, each file in its own transaction |

Seeds run automatically when `nexus dev` creates a fresh database, and any
time with `nexus db seed`. Keep them idempotent (`on conflict do nothing`).

## dev

| key | default | |
|---|---|---|
| `auto_migrate` | `true` | apply pending migrations on `nexus dev` |
| `auto_seed` | `true` | seed a freshly created local database |

## environments

The local environment is implicit and configured by `database`. Remote
environments are named targets you select with `--env <name>` (or
`NEXUS_ENV`).

| key | default | |
|---|---|---|
| `database_url` | required | connection string, usually `${VAR}` |
| `protected` | `true` for `production` and `prod`, else `false` | protected environments require a typed phrase for destructive operations and confirmation for writes |

### Secrets and interpolation

`${VAR}` and `${VAR:-default}` are read from your shell environment **at the
moment an environment is used**, never at load time. So:

- a missing production secret never breaks local work;
- a missing secret for the environment you target fails loudly, naming the
  variable — Nexus never substitutes an empty string or another
  environment's value.

Local credentials are generated per project and stored in
`.nexus/local.json` with `0600` permissions. `nexus init` adds `.nexus/` to
`.gitignore`, and `nexus doctor` fails if it isn't ignored.

## Diagnostics

Unknown keys produce a warning on stderr whenever the project is loaded, and
in `nexus doctor`, with suggestions:

```
line 7: unknown key "databse" (did you mean "database"?)
```

## Environment variables

| variable | |
|---|---|
| `NEXUS_ENV` | default environment (instead of `local`) |
| `NEXUS_DATABASE_URL` | connect to this database instead of the project's |
| `NEXUS_PG_BIN` | directory containing PostgreSQL binaries |
| `NEXUS_THEME` | `light` or `dark` (otherwise `COLORFGBG`, then dark) |
| `NEXUS_PLAIN`, `NEXUS_NO_ANIMATION` | same as `--plain`, `--no-animation` |
| `NEXUS_LOG` | `debug`, `info`, `warn` or `error` diagnostics on stderr |
| `NO_COLOR`, `FORCE_COLOR` | standard colour switches |
| `CI` | disables animation and prompts |

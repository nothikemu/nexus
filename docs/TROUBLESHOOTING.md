# Troubleshooting

First, run:

```bash
nexus doctor
```

It checks configuration, PostgreSQL, Docker, the connection, migrations,
schema health, secrets and your terminal, and says what to do about each
problem. For more detail on any command, add `--verbose`.

## Installing

**`nexus: command not found`** — the install folder isn't on your `PATH`.
The installer prints the line to add, e.g.
`export PATH="$HOME/.local/bin:$PATH"` (put it in `~/.bashrc` / `~/.zshrc`).

**The install script says there's no release for my platform** — it falls
back to building from source when Go 1.24+ is installed. Otherwise install Go
or build from a checkout (see [INSTALL.md](INSTALL.md)).

## Starting the database

**"PostgreSQL isn't installed."** — Install it (`brew install postgresql@16`,
`sudo apt install postgresql`, Postgres.app…), use Docker
(`database.runtime: docker` in `nexus.yaml`), or point at an existing
database (`database.url`). If PostgreSQL is installed somewhere unusual:
`export NEXUS_PG_BIN=/path/to/bin`.

**"PostgreSQL won't run as root."** — PostgreSQL refuses to run as root.
Run Nexus as a normal user, or use Docker. (In `auto` mode Nexus picks Docker
for you when running as root.)

**"port 54320 is taken."** — Something else is listening there (perhaps
another project). Change `database.port` in `nexus.yaml`.

**"docker isn't available."** — Start Docker Desktop / the Docker daemon.

**"this project's cluster was created with PostgreSQL 15, which is no longer
installed"** — A local cluster can only be started by the major version that
created it. Reinstall that version, or start fresh with `nexus dev reset`
(this deletes local data).

**It started but something looks wrong** — `nexus dev logs` (or `-f` to
follow) shows the database's own log, coloured by severity.

## Connecting

**"nexus is asleep."** — The local database isn't running. `nexus up`.
On the dashboard, press `w`.

**"the database rejected the credentials."** on local — the password in
`.nexus/local.json` doesn't match the database (for example, you copied the
folder or recreated the database by hand). `nexus dev reset` starts clean.

**"environment variable X isn't set."** — That environment's
`database_url` uses `${X}`. Export it in your shell or CI secrets.

**"there's no staging environment."** — Add it under `environments:` in
`nexus.yaml`.

**"not inside a nexus project."** — Run commands inside the project (or a
subfolder), pass `-C path/to/project`, or use `--db-url` to work with any
database without a project.

## Migrations

**"applied migrations changed on disk."** — Someone edited a migration
after it was applied. Restore the file (`git checkout`), or if the edit was
harmless, accept it with `nexus migration repair`.

**"pending migrations are older than the latest applied one."** — Usually
after merging branches. Check they don't conflict, then
`nexus migration apply --allow-out-of-order`.

**"can't roll back … it has no -- nexus:down section."** — Write the down
section, or write a new migration that undoes the change.

**"transaction control isn't allowed"** — Remove `BEGIN`/`COMMIT` from the
migration; Nexus wraps each one in a transaction already. For statements that
must run outside a transaction, add `-- nexus:no-transaction`.

**"CREATE INDEX CONCURRENTLY cannot run inside a transaction block"** — Add
`-- nexus:no-transaction` to that migration file.

**"another migration is running."** — Another process holds the migration
lock (perhaps a deploy). Wait and retry.

## In scripts and CI

**Exit code 3, "this needs confirmation."** — The command is destructive and
there's no terminal to ask. Add `--yes`, or on a protected environment
`--confirm "DELETE PRODUCTION DATA"`.

**Colour codes in my logs** — Nexus disables colour when output isn't a
terminal; if something forces it on, use `--plain` or `NO_COLOR=1`.

**I need to parse the output** — Use `--json`. Errors are JSON on stderr:
`{"error": {"title": …, "detail": …, "hint": …, "code": …}}`.

## The terminal looks wrong

**Boxes or odd characters instead of Nex** — your font lacks block
elements. Use a modern font (most are fine: SF Mono, Menlo, JetBrains Mono,
Cascadia, DejaVu Sans Mono…), or `--no-color` for line art, or `--plain`.

**Colours are hard to read on my light background** — `export NEXUS_THEME=light`.

**Animation is distracting** — `--no-animation` or `export NEXUS_NO_ANIMATION=1`.

**Misaligned characters in a CJK locale** — some terminals draw "ambiguous
width" symbols as double width. Use `--no-color` or `--plain`.

## Still stuck?

Open an issue with the output of `nexus version` and `nexus doctor --plain`
(it never prints passwords).

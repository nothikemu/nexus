# FAQ

**What is Nexus, in one sentence?**
A command-line tool that runs and manages PostgreSQL for your project —
migrations, browsing, querying, performance — with a friendly terminal UI
(and a mascot named Nex).

**Do I need to know SQL?**
It helps, but you can get a long way without it: `nexus tables`,
`nexus browse <table>` (search, filter, sort, edit) and `nexus table <name>`
show your data and structure. Migrations are SQL files, and the
[getting started guide](GETTING-STARTED.md) walks through writing one.

**Is it a database? Will it replace PostgreSQL?**
No — it *is* PostgreSQL underneath. Nexus runs a normal PostgreSQL server and
everything it does is plain SQL you could run yourself. Any PostgreSQL tool
(psql, your ORM, a GUI) works alongside it: `nexus db url` prints the
connection string.

**Where does my data live?**
With the native runtime, in your project's `.nexus/postgres/` folder. With
Docker, in a named volume (`nexus-<project>-pgdata`). With an external
database, wherever that database lives. `nexus dev status` tells you.

**Is it safe to use against production?**
Nexus is built to be careful there: production environments are protected by
default, destructive operations need a typed phrase (`--yes` isn't enough),
explorer editing is disabled, `EXPLAIN ANALYZE` always rolls back, and filters
run read-only. Still: preview with `nexus migration diff` first, and use CI
for production changes. See [environments](guides/environments.md).

**Does Nexus phone home?**
No. Nexus makes no network requests except to the databases you point it at
(and Docker, if you use it).

**Can I commit `nexus.yaml`?**
Yes — that's the intent. Secrets go in environment variables (`${VAR}`), and
the generated local password lives in the git-ignored `.nexus/` folder.

**Can I use it with an existing database, without a project?**
Yes: `nexus --db-url postgres://… db inspect`, `… browse orders`,
`… query analyze "…"`, `… sql`. Or `export NEXUS_DATABASE_URL=…`.

**Can I use my ORM's migrations instead?**
Yes. Skip `nexus migration` and keep using `nexus up`, the explorer, the SQL
shell, the analyzer and the dashboard. (Set `dev.auto_migrate: false` if you
have no Nexus migrations.)

**Which PostgreSQL versions are supported?**
13 to 18. CI tests 13, 16 and 17.

**Windows?**
Nexus builds for Windows and the runtimes support it, but Linux, macOS and
WSL are the most tested.

**Nex is cute but I'd like a quiet tool.**
`--plain` (or `NEXUS_PLAIN=1`) removes the mascot, colour and animation.
`--no-animation` keeps the looks but stops the motion.

**What about auth, APIs, storage, realtime, functions…?**
They're designed (see [ARCHITECTURE.md](ARCHITECTURE.md)) and come in later
phases. Nexus never shows a service it doesn't actually run.

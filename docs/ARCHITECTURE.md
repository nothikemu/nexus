# Nexus Architecture

> A serious backend that happens to have a soul.

This document is the architectural contract for Nexus. It describes the whole
system across all six phases, and marks precisely what exists today
(**Phase 1**) versus what is designed but not yet built. Nothing in the CLI
pretends a later-phase feature exists.

---

## 1. Principles

1. **PostgreSQL is the foundation.** Nexus never re-implements SQL. Every
   feature — auth, storage metadata, jobs, events — is modelled as PostgreSQL
   schemas, tables, functions and policies, so it is inspectable with plain SQL.
2. **One binary.** The `nexus` CLI, dev supervisor, API gateway and workers ship
   as a single static Go binary. No runtime to install besides PostgreSQL
   (native, Docker, or remote).
3. **Two personalities.** The core is strict, typed and tested. The surface —
   colour, mascot, voice, animation — is a separate layer (`internal/ui`,
   `internal/tui`) that never influences behaviour. `--plain` and `--json`
   strip the surface entirely and the product works identically.
4. **Safe by default.** Destructive operations are classified, previewed and
   confirmed. Protected environments demand a typed phrase. Secrets are scoped
   to exactly one environment.
5. **Every phase ships working.** Unbuilt features have clean interfaces and
   are reported honestly (e.g. `nexus status` lists only real services).

---

## 2. Language & frameworks

| Concern            | Choice                                  | Why |
|--------------------|-----------------------------------------|-----|
| Language           | Go 1.24                                 | Fast startup, single static binary, first-class concurrency, cross-compiles to every terminal Nexus targets. |
| CLI framework      | `spf13/cobra`                           | Mature command tree, completions, groups; help is re-skinned by Nexus. |
| Styling            | `charmbracelet/lipgloss` + `termenv`    | Colour-profile degradation (truecolor → 256 → 16 → none). |
| Interactive TUI    | `charmbracelet/bubbletea` + `bubbles`   | Elm-style state machines: testable, composable views. |
| PostgreSQL         | `jackc/pgx/v5` (+ `pgxpool`)            | Native protocol, cancellation, text-format results, COPY, LISTEN/NOTIFY. |
| Config             | `gopkg.in/yaml.v3`                      | Node-level access for unknown-key diagnostics. |
| Logging            | `log/slog`                              | Structured, levelled, zero-dependency. |

---

## 3. Repository layout

```
cmd/nexus/              entrypoint (tiny; calls cli.Execute)
internal/
  version/              build metadata (ldflags)
  logging/              slog setup (--verbose, NEXUS_LOG)
  textutil/             small shared string algorithms
  ui/                   the visual system: modes, palette, theme, glyphs,
                        printer, panels, tables, kv, trees, sparklines,
                        spinner tasks, prompts, problems, code frames, voice
  ui/mascot/            the Nexus core: sprites, expressions, poses, animation
  render/               domain renderers shared by CLI and TUIs: result sets,
                        schemas, plans, migrations, diffs, SQL problems
  tui/                  Bubble Tea applications
    repl/               interactive SQL shell
    browser/            table explorer (rows, search, sort, JSON, FK nav, edit)
    dashboard/          `nexus` live dashboard
    tuikit/             shared TUI helpers
  config/               nexus.yaml schema, defaults, validation, interpolation
  project/              project discovery, scaffolding, local state
  safety/               operation levels, SQL classification, confirmation
  pg/                   connections, text-format query execution, errors
  pg/introspect/        catalog snapshots (tables, columns, indexes, FKs, RLS…)
  pg/schemadiff/        snapshot → snapshot structural diff
  pg/explain/           EXPLAIN JSON parsing, analysis & recommendations
  pg/stats/             overview, activity and schema health checks
  pg/rows/              paged, searched, filtered reads in read-only transactions
  pgtest/               throwaway databases for integration tests
  sqltext/              statement splitting, completeness, error positions
  migrate/              migration files, history, engine, locks
  localdb/              local PostgreSQL runtimes: native, docker, external
  cli/                  cobra commands (one file per command group)
docs/                   architecture, configuration, design language, CLI reference
```

Dependency direction is strictly downward:
`cli → tui → render → (migrate, localdb, pg/*, project, safety) → (config, sqltext) → ui`.
`ui` depends on nothing internal except `textutil`; `ui/mascot` depends only on `ui`.
The `pg/*` packages never import `ui`.

---

## 4. CLI architecture

* **Root context (`cli.App`)** — created once per invocation. Holds the output
  `ui.Printer`, resolved global flags, and *lazily* loads the project and opens
  a pgx pool on first use (commands that don't need a database never connect).
* **Global flags** — `--json`, `--plain`, `--no-color`, `--no-animation`,
  `--env/-e`, `--db-url`, `--project/-C`, `--yes/-y`, `--confirm`,
  `--verbose/-v`. Environment equivalents: `NO_COLOR`, `NEXUS_ENV`,
  `NEXUS_DATABASE_URL`, `NEXUS_LOG`, `CI`.
* **Result rendering** — commands produce typed result structs. In `--json`
  mode they are marshalled to stdout; otherwise rendered by the UI layer.
  Progress, spinners and prompts go to **stderr** so stdout stays pipeable.
* **Errors** — everything funnels into `ui.Problem` (title, detail, hint,
  code frame). PostgreSQL errors carry SQLSTATE, detail, hint and a caret
  under the failing SQL. Exit codes: `0` ok, `1` failure, `2` usage,
  `3` declined/aborted, `4` database unreachable, `130` interrupted.
* **Help** — Cobra help is re-templated with Nexus typography: grouped
  commands, examples on every command, `--json` noted where supported.

---

## 5. Terminal UI architecture

### Modes (`ui.Mode`)

| Mode            | Colour | Unicode | Animation | Mascot | Prompts |
|-----------------|:------:|:-------:|:---------:|:------:|:-------:|
| rich (TTY)      | ✓      | ✓       | ✓         | ✓      | ✓       |
| `--no-animation`| ✓      | ✓       | –         | static | ✓       |
| `--no-color`    | –      | ✓       | ✓         | line-art | ✓     |
| `--plain`       | –      | ASCII   | –         | –      | ✓       |
| `--json`        | –      | –       | –         | –      | –       |
| non-TTY / `CI`  | auto   | ✓       | –         | static | –       |

Colour depth degrades automatically (truecolor → ANSI256 → ANSI16) via
termenv. Light terminals are supported via `NEXUS_THEME=light` or
`COLORFGBG`; Nexus never blocks on terminal background queries.

### Components (`internal/ui`)

`Printer` (spacing-aware block output) · `Say` (voice messages with glyph
tones) · `Panel` (rounded, titled) · `Table` (width-aware, numeric
alignment, truncation) · `KV` · `Tree` · `Sparkline` · `Bar` · `Badge` ·
`Dot` · `Task` (spinner → ✓/✕ with timing) · `Confirm`/`ConfirmPhrase` ·
`Problem` (errors with code frames) · `Gradient` · `Wordmark`.

### Voice

Nexus speaks in lowercase, short, calm sentences. A small catalogue
(`ui/voice.go`) holds the phrases; variation is limited and never random in
tests. Rules: no exclamation-mark spam, no emoji, never sarcastic, never
more than one line of personality per command.

| Glyph | Tone     | Use |
|:-----:|----------|-----|
| ✦     | nexus    | Nexus speaking: awake, found, connected |
| ◈     | success  | completed, insight, "that's better" |
| ▲     | warning  | needs attention |
| ✕     | error    | failed |
| ◇     | info     | neutral information |
| →     | hint     | next step |

---

## 6. Mascot system (`internal/ui/mascot`)

The mascot is **the Nexus core**: a small luminous data-being — a rounded
violet-to-cyan core with two eyes, a spark above it, and two network links
reaching out to nodes. It is, literally, a nexus: the thing in the middle that
everything connects to.

```
       ✦
    ▗▄▄▄▄▄▖
   ▟ ◉   ◉ ▙
●──▜       ▛──●
    ▝▀▀▀▀▀▘
```

* **Renderings:** `Portrait` (15×5, solid colour via background cells and
  quadrant/three-quadrant blocks, so the body reads as a bevelled core),
  `Face` (single-line pill, 7 cells), `Glyph` (1 cell).
  No-colour mode swaps to a line-art sprite; plain mode omits the mascot.
* **States:** idle, thinking, working, success, warning, error, sleeping,
  connecting, celebrating, curious. Each state defines eyes, mouth, spark,
  pose (arms level / raised / lowered / broken) and tint.
* **Animation:** frame functions (`Frame(state, n)`) — blinking, twinkling
  spark, packets travelling along links, drifting `z`s. Animations are short
  (< 1 s), only on TTYs, and disabled by `--no-animation`/CI.

---

## 7. Configuration (`nexus.yaml`)

```yaml
project:
  name: my-app

database:
  version: 16            # PostgreSQL major for local development
  runtime: auto          # auto | native | docker | external
  port: 54320
  name: my_app
  schemas: [public]
  # url: ${DATABASE_URL} # runtime: external

migrations:
  dir: migrations

seeds:
  paths: [seeds/*.sql]

dev:
  auto_migrate: true
  auto_seed: true

environments:
  staging:
    database_url: ${NEXUS_STAGING_DATABASE_URL}
  production:
    database_url: ${NEXUS_PRODUCTION_DATABASE_URL}
    protected: true
```

* `${VAR}` / `${VAR:-default}` interpolation is resolved lazily, only for the
  environment in use — a missing production secret never breaks local work,
  and one environment's values are never used as another's fallback.
* Unknown keys produce warnings with "did you mean" suggestions.
* Local credentials are generated per project and stored in
  `.nexus/local.json` (mode 0600, git-ignored), never in `nexus.yaml`.

---

## 8. Database architecture

### Internal schemas

Nexus owns schemas prefixed `nexus`; user schemas are never modified except
by user-authored migrations.

| Schema           | Phase | Contents |
|------------------|:-----:|----------|
| `nexus_meta`     | 1     | `migrations`, `migration_events` (not named `nexus`: the default `search_path` begins with `"$user"`, so a schema named after a plausible role would capture unqualified objects) |
| `nexus_auth`     | 2     | users, identities, sessions, refresh_tokens, mfa_factors, api_keys, organizations, memberships, roles |
| `nexus_storage`  | 2     | buckets, objects (metadata; bytes live in the blob backend), upload sessions |
| `nexus_realtime` | 2     | channels, presence, publications |
| `nexus_jobs`     | 3     | queues, jobs, attempts, schedules (cron), dead letters |
| `nexus_events`   | 3     | event log (append-only, partitioned by time), subscriptions, webhook endpoints, deliveries |
| `nexus_vault`    | 3     | encrypted secrets per environment |
| `nexus_ops`      | 4     | backups, branches, audit log |

### Phase 1 tables

```sql
create table nexus_meta.migrations (
  version      text primary key,
  name         text        not null,
  checksum     text        not null,          -- sha256 of the up section
  applied_at   timestamptz not null default now(),
  duration_ms  integer     not null,
  applied_by   text        not null default current_user,
  nexus_version text       not null
);

create table nexus_meta.migration_events (
  id          bigint generated always as identity primary key,
  version     text        not null,
  name        text        not null,
  action      text        not null check (action in ('apply','rollback','repair')),
  at          timestamptz not null default now(),
  duration_ms integer,
  actor       text        not null default current_user
);
```

### Migrations

* Files: `migrations/<14-digit UTC version>_<name>.sql` with
  `-- nexus:up` / `-- nexus:down` sections; `-- nexus:no-transaction` for
  statements like `CREATE INDEX CONCURRENTLY`.
* Each migration runs in its own transaction (transactional DDL), under a
  session-level advisory lock so two deploys can never interleave.
* **Checksums** detect edited-after-apply files; **repair** reconciles them.
* **Dry run / diff**: pending migrations execute inside a transaction that is
  always rolled back; Nexus snapshots the catalog before and after and reports
  the structural diff. Nothing is persisted.
* **Out-of-order** migrations (from merged branches) are detected and require
  `--allow-out-of-order`.
* Failures render the failing statement with a caret at the PostgreSQL error
  position, mapped back to the file's line and column.

### Local runtimes (`internal/localdb`)

| Runtime    | How |
|------------|-----|
| `native`   | Discovers PostgreSQL binaries (PATH, `pg_config`, Homebrew, Postgres.app, apt, Windows). `initdb` into `.nexus/postgres/data`, `pg_ctl` start/stop, TCP-only on 127.0.0.1, SCRAM auth, Nexus settings in an included `nexus.conf`. |
| `docker`   | `postgres:<version>` container per project with a named volume, bound to 127.0.0.1. |
| `external` | Any reachable `database.url`; Nexus never starts or stops it. |
| `auto`     | native → docker → explain how to proceed. |

---

## 9. API architecture (Phase 2+)

A single HTTP server (`nexus serve`, supervised by `nexus dev`) composed of
middleware stages — the **gateway pipeline**:

```
request → CORS → rate limit → authenticate (JWT | API key | session)
        → authorize (policy engine + RLS claims) → validate → route
        → { REST | GraphQL | SQL | storage | functions } → log/metrics/trace
```

* **REST** is generated from the introspection snapshot:
  `GET/POST /api/{table}`, `GET/PATCH/DELETE /api/{table}/{pk}` with
  filtering (`?email=eq.x`), ordering, pagination and embedding via FKs.
  Every request executes as `SET LOCAL ROLE nexus_authenticated` with
  `request.jwt.claims` so **PostgreSQL RLS is the enforcement point**.
* **OpenAPI 3.1** and **GraphQL** schemas are derived from the same snapshot,
  as are generated SDKs (`nexus generate <lang>`).

## 10. Authentication & authorization (Phase 2)

* Identities: email/password (argon2id), magic links, OAuth/OIDC, passkeys
  (WebAuthn), TOTP + recovery codes, phone OTP via provider interface, SAML
  via a provider plugin.
* Sessions: short-lived JWT access tokens (EdDSA, rotating keys published at
  JWKS) + opaque refresh tokens stored hashed with reuse detection.
* API keys and service accounts are hashed, scoped and environment-bound.
* Authorization: RBAC with role inheritance + ABAC conditions compiled to
  RLS policies where possible; the policy engine evaluates the rest.
  `nexus policy test` traces each rule to explain grant/deny.

## 11. Event architecture (Phase 3)

```
 Database (logical decoding) ─┐
 Auth / Storage / API ────────┼──► nexus_events.log ──► dispatcher
 Functions (emit) ────────────┘        (outbox)          ├─► jobs
                                                         ├─► webhooks (signed, retried)
                                                         ├─► functions
                                                         └─► realtime / integrations
```

* Transactional **outbox**: events are inserted in the same transaction as
  the change that caused them — no dual-write inconsistencies.
* At-least-once delivery, per-subscriber cursors, idempotency keys, replay
  from any offset, schemas per event type, dead-letter queues.
* Jobs use `SELECT … FOR UPDATE SKIP LOCKED` with priorities, delays,
  exponential backoff, dependencies and cancellation. Cron schedules are rows.

## 12. Plugin architecture (Phase 5)

Plugins are separate executables (`nexus-plugin-<name>`) speaking a versioned
JSON-RPC protocol over stdio — the same isolation model as `git`/`kubectl`
plugins, language-agnostic and crash-isolated. A manifest declares
contributed commands, event subscriptions, functions and dashboard panels.
Plugins receive scoped, environment-bound credentials — never the project's
master secrets.

## 13. Testing strategy

| Layer | How |
|-------|-----|
| Unit | Pure packages: `config`, `sqltext`, migration parsing, `schemadiff`, `explain` analysis, health checks, SQL safety classification, `ui` rendering in every mode, mascot geometry (every frame of every state is exactly 15×5). |
| Integration | Real PostgreSQL via `NEXUS_TEST_DATABASE_URL`; each test gets its own throwaway database (`internal/pgtest`), and skips with a reason when the variable is unset. Covers introspection, the migration engine (drift, repair, rollback, previews, error positions, partial-failure rollback), EXPLAIN ANALYZE rollback of writes, and read-only row filters. |
| Runtime | `localdb` starts, restarts and stops a real native cluster when binaries exist and the user isn't root. |
| CLI | Commands run in-process (`cli.Run`) against a scaffolded project; JSON output, exit codes and protected-environment rules asserted. A docs test keeps `docs/CLI.md` in step with the command tree. |
| CI | `gofmt`, `go vet`, `staticcheck`; `go test -race` against PostgreSQL 13, 16 and 17 service containers; builds on Linux, macOS and Windows. |

---

## 14. Phases

| Phase | Scope | Status |
|------:|-------|--------|
| 1 | CLI, branding, mascot, UI system, init, config, local PostgreSQL, migrations, introspection, SQL shell, table explorer, explain analyzer, dashboard, status, doctor | **built** |
| 2 | REST API, OpenAPI, generated types, authentication, authorization, storage, realtime | designed |
| 3 | Functions, jobs, queues, cron, webhooks, event bus | designed |
| 4 | Backups, branching, environments, observability, security center | designed (environment targeting & protection exist) |
| 5 | Vectors, AI copilot, search, code generation, plugins | designed |
| 6 | Cloud deployment, self-hosting, replication, scaling | designed |

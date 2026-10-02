# Staging, production and CI

Your project always has a **local** environment — the database `nexus up`
runs. You can add remote ones and point any command at them with `--env`.

## Adding environments

```yaml
# nexus.yaml
environments:
  staging:
    database_url: ${NEXUS_STAGING_DATABASE_URL}
  production:
    database_url: ${NEXUS_PRODUCTION_DATABASE_URL}
    protected: true          # the default for "production" and "prod"
```

```bash
export NEXUS_STAGING_DATABASE_URL="postgres://…"
nexus --env staging status
nexus --env staging migration apply
NEXUS_ENV=staging nexus sql
```

## Secrets

`${VARS}` are read from your shell **only when that environment is used**:

- a missing production secret never gets in the way of local work;
- targeting an environment whose secret is missing fails loudly, naming the
  variable. Nexus never substitutes an empty value, and never uses one
  environment's settings for another.

Never put real connection strings in `nexus.yaml` — it's meant to be
committed.

## Protected environments

Protected environments add guard rails:

| operation | local / unprotected | protected |
|---|---|---|
| reads | — | — |
| writes (apply migrations, seed, writing SQL) | — | asks y/N (`--yes` skips) |
| destructive (rollback, drop, truncate, delete…) | asks y/N (`--yes` skips) | must type `DELETE PRODUCTION DATA` |
| editing cells in the explorer | allowed | disabled |

`--yes` is **never** enough for a destructive operation on a protected
environment. In CI, pass the exact phrase:

```bash
nexus --env production migration rollback --confirm "DELETE PRODUCTION DATA"
```

## CI

Nexus behaves well in CI: no colour codes or animation when there's no
terminal (or `CI` is set), stable `--json` output, and meaningful exit codes.

| exit code | meaning |
|---|---|
| 0 | success |
| 1 | failed |
| 2 | usage error (bad flag, unknown environment, no project) |
| 3 | needs confirmation, or was declined |
| 4 | database unreachable |
| 130 | interrupted |

### GitHub Actions: check and apply migrations

```yaml
jobs:
  migrate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | sh
      - name: preview
        run: nexus --env staging --plain migration diff
        env:
          NEXUS_STAGING_DATABASE_URL: ${{ secrets.STAGING_DATABASE_URL }}
      - name: apply
        run: nexus --env staging --plain migration apply --yes
        env:
          NEXUS_STAGING_DATABASE_URL: ${{ secrets.STAGING_DATABASE_URL }}
```

### Test migrations against a throwaway database

```yaml
    services:
      postgres:
        image: postgres:16
        env: { POSTGRES_PASSWORD: postgres }
        ports: ["5432:5432"]
        options: --health-cmd pg_isready --health-interval 2s --health-retries 20
    steps:
      - uses: actions/checkout@v4
      - run: curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | sh
      - run: |
          url=postgres://postgres:postgres@localhost:5432/postgres
          nexus --plain --db-url "$url" migration apply      # every migration applies cleanly
          nexus --plain --db-url "$url" migration rollback --steps 999 --yes   # and every down works
```

> `--db-url` overrides the environment's database for one command, which is
> handy for scratch databases.

### Reading JSON

```bash
pending=$(nexus --env staging --json migration status | jq .pending)
nexus --json db inspect | jq '.problems[].title'
```

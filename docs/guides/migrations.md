# Migrations in depth

A migration is a SQL file that changes your schema, plus (optionally) how to
undo it. Nexus applies them in order, exactly once per database, and
remembers which ran.

## The workflow

```bash
nexus migration create add_profiles   # 1. write it
nexus migration diff                  # 2. preview it (nothing is saved)
nexus migration apply                 # 3. apply it
nexus migration status                # where am I?
nexus migration rollback              # undo the most recent one
```

`nexus up` also applies pending migrations automatically (turn that off with
`dev.auto_migrate: false`).

## Anatomy of a migration file

`migrations/20261002141500_add_profiles.sql` — the number is a UTC
timestamp that orders migrations; the rest is a name for humans.

```sql
-- profiles for users        ← comments above the marker are fine

-- nexus:up
create table public.profiles (
  user_id uuid primary key references public.users (id) on delete cascade,
  bio     text,
  avatar  text
);

-- nexus:down
drop table public.profiles;
```

- **`-- nexus:up`** — what to apply. Required (a file with no markers is
  treated as all-up).
- **`-- nexus:down`** — how to undo it. Optional, but without it
  `rollback` refuses ("no -- nexus:down section").
- **`-- nexus:no-transaction`** — see below.

## What makes them safe

**Transactions.** Each migration's up section runs in one transaction. If
statement 7 of 10 fails, statements 1–6 are rolled back too, and no later
migration runs. Your database is never left half-migrated.

**Exact errors.** A failure points at the file, line and column:

```
✕ migration 20261002145504_add_posts failed.

  type "integr" does not exist
  migrations/20261002145504_add_posts.sql:11:14 (up section, rolled back)

  11 │   views      integr not null default 0
     │              ^
```

**Previews.** `nexus migration diff` (or `apply --dry-run`) runs pending
migrations inside a transaction, compares the schema before and after, then
rolls back. You see tables, columns, indexes, constraints, policies, enums
and functions that would be added, changed or removed.

**Locking.** An advisory lock means two `apply` runs — say, two deploys —
can never interleave. The second waits, then reports that a migration is in
progress.

**Checksums.** Nexus records a fingerprint of every applied migration. If
someone edits an applied file, `apply` stops:

```
✕ applied migrations changed on disk.
  ~ 20261002141500_init  edited after it was applied
  → restore the files, or accept the current state with nexus migration repair
```

Editor noise (line endings, trailing spaces) doesn't count as an edit.

## Statements that can't run in a transaction

`CREATE INDEX CONCURRENTLY`, `ALTER TYPE … ADD VALUE` (before PostgreSQL 12)
and a few others refuse to run inside a transaction. Mark the file:

```sql
-- nexus:no-transaction
-- nexus:up
create index concurrently posts_title_idx on public.posts (title);

-- nexus:down
drop index concurrently public.posts_title_idx;
```

Statements then run one at a time. If one fails, earlier ones in that file
have already happened — keep these migrations small (ideally one statement).
Previews skip them, since they can't run inside a transaction.

Writing `BEGIN`/`COMMIT` yourself in a normal migration is rejected with an
explanation, because it would break Nexus's own transaction.

## Branches and out-of-order migrations

Two people create migrations on separate branches. After merging, one of
them may be *older* than a migration that's already applied. Nexus notices:

```
✕ pending migrations are older than the latest applied one.
  → apply them anyway with nexus migration apply --allow-out-of-order
```

Check they don't conflict, then pass the flag.

## Rolling back

```bash
nexus migration rollback            # the most recent
nexus migration rollback --steps 3  # the last three, newest first
```

Rollback is destructive (it usually drops things), so Nexus shows what it
will undo and asks. In scripts, pass `--yes`; on a protected environment
you'll need the typed phrase (see [environments](environments.md)).

## Repair

When history and files disagree on purpose — you fixed a comment in an
applied migration, or deleted an obsolete file — `repair` reconciles them:

```bash
nexus migration repair
```

It only updates Nexus's bookkeeping (accepts new checksums, forgets deleted
files). It never touches your schema.

## Where history lives

In your database, in the `nexus_meta` schema:

```sql
select * from nexus_meta.migrations;        -- what's applied, when, by whom, how long
select * from nexus_meta.migration_events;  -- every apply, rollback and repair
```

## Applying to staging and production

```bash
nexus --env staging migration diff
nexus --env staging migration apply
nexus --env production migration apply     # asks for confirmation
```

In CI: see [environments and CI](environments.md#ci).

## Tips

- Keep migrations small and focused; one idea per file.
- Always write the `down` section while the `up` is fresh in your mind.
- Run `nexus migration diff` before every apply — it's free.
- Never edit a migration that's been applied anywhere shared. Write a new one.

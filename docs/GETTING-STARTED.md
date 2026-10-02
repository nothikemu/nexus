# Getting started

In about five minutes you'll create a project, start a database, change its
schema, look at your data and make a slow query fast. Nex will keep you
company.

You need `nexus` installed and PostgreSQL or Docker available — see
[INSTALL.md](INSTALL.md). If `nexus doctor` is happy, you're ready.

## 1. Create a project

```bash
nexus init blog
```

Nex wakes up and creates:

```
blog/
├── nexus.yaml                      # project configuration
├── migrations/
│   └── 20261002120000_init.sql     # your first migration: a users table
├── seeds/
│   └── seed.sql                    # three example users
└── .gitignore                      # keeps .nexus/ (local data, passwords) out of git
```

On an interactive terminal Nexus then asks **"start the database now?"** —
press enter and skip step 2.

## 2. Start the database

```bash
cd blog
nexus up
```

`nexus up` (also `nexus dev`):

1. creates a private PostgreSQL database for this project (the first time),
2. applies every migration that hasn't run yet,
3. loads the seed data into a fresh database,
4. shows you what's running.

The database keeps running in the background. Stop it any time with
`nexus down`; your data stays.

> Want your app to connect to it? `export DATABASE_URL=$(nexus db url)`.

## 3. Look around

```bash
nexus tables              # your tables
nexus table users         # columns, indexes, relationships
nexus sql "select * from users"
```

Or open the dashboard — just type:

```bash
nexus
```

Press `2` for tables, use `↑`/`↓`, and `enter` to browse one. `q` quits.

## 4. Change the schema

Let's add posts. Create a migration:

```bash
nexus migration create add_posts
```

Open the new file in `migrations/` and fill in both halves — how to apply
the change, and how to undo it:

```sql
-- nexus:up
create table public.posts (
  id         bigint generated always as identity primary key,
  author_id  uuid not null references public.users (id) on delete cascade,
  title      text not null,
  body       text,
  views      integer not null default 0,
  created_at timestamptz not null default now()
);

-- nexus:down
drop table public.posts;
```

Preview it first. This runs the migration inside a transaction, shows what
would change, and rolls it all back — nothing is saved:

```bash
nexus migration diff
```

```
◈ preview complete — nothing was changed.

  1 migration · 1 table changed · 2 additions

  + table public.posts  6 columns
  + sequence public.posts_id_seq
```

Happy? Apply it:

```bash
nexus migration apply
```

Made a typo? Nexus shows the exact line and column, and the migration is
rolled back completely — fix the file and apply again.

Changed your mind? `nexus migration rollback` runs the `down` section.
(It asks first, because it deletes data.)

## 5. Add some data and explore it

```bash
nexus sql "insert into posts (author_id, title, views)
           select (select id from users order by random() limit 1), 'post ' || g, g % 1000
           from generate_series(1, 50000) g"
```

Now open the explorer:

```bash
nexus browse posts
```

| key | does |
|---|---|
| `↑↓←→` | move around |
| `enter` | see the whole row (JSON is pretty-printed) |
| `/` | search every column |
| `f` | filter with SQL, e.g. `views > 900` |
| `s` | sort by the selected column |
| `g` | on `author_id`: jump to that user |
| `e` | edit the selected cell (saved by primary key, after you confirm) |
| `esc` | go back · `q` quits |

## 6. Make a slow query fast

```bash
nexus query analyze "select * from posts where title = 'post 4242'"
```

Nexus runs the query (in a transaction it rolls back), shows where the time
went, and notices the problem:

```
✦ nexus noticed something.

  ▲ posts has no index on title
    scanned 50,000 rows to return 1. An index on title lets PostgreSQL
    jump straight to matching rows.

      create index concurrently posts_title_idx on "public"."posts" ("title");
```

Put that in a migration. Because `create index concurrently` can't run inside
a transaction, mark the file:

```sql
-- nexus:no-transaction
-- nexus:up
create index concurrently posts_title_idx on public.posts (title);

-- nexus:down
drop index concurrently public.posts_title_idx;
```

Apply it and run the analysis again — it now says `uses index posts_title_idx`.

## 7. Check in on things

```bash
nexus status       # what's running, schema, migrations, health
nexus db inspect   # deeper health checks with suggested fixes
nexus doctor       # is my whole setup OK?
nexus hi           # Nex's take, plus a tip
```

## Where next

- [The SQL shell](guides/sql-shell.md)
- [Migrations in depth](guides/migrations.md)
- [The table explorer](guides/table-explorer.md)
- [The dashboard](guides/dashboard.md)
- [Making queries fast](guides/performance.md)
- [Staging, production and CI](guides/environments.md)
- [All commands](CLI.md) · [Configuration](CONFIG.md) · [Troubleshooting](TROUBLESHOOTING.md)

And when you need a reminder of everything, `nexus guide` fits it on one
screen.

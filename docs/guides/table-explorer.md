# The table explorer

```bash
nexus browse users          # or: nexus table users browse
```

A full-screen view of a table's rows that you can search, filter, sort,
inspect, follow and edit — without writing SQL.

## Keys

| key | does |
|---|---|
| `↑` `↓` | move between rows (and pages, at the edges) |
| `←` `→` | move between columns; the view scrolls sideways |
| `home` / `end` | first / last column |
| `n` / `p` (or `pgdn`/`pgup`) | next / previous page |
| `enter` | open the whole record |
| `/` | search every column (case-insensitive) |
| `f` | filter with a SQL condition |
| `s` | sort by the selected column: ascending → descending → off |
| `g` | follow the foreign key under the cursor |
| `e` | edit the selected cell |
| `r` | refresh |
| `esc` | back (from a followed table, or clear the search/filter) |
| `q` | quit |

## Searching and filtering

- **Search** (`/`) looks for your text in every column, including numbers,
  dates and UUIDs (each value is compared as text). `%` and `_` are taken
  literally.
- **Filter** (`f`) takes any SQL condition: `created_at > now() - interval '1 day'`,
  `email like '%@example.com'`, `views between 10 and 20`.

Every read runs in a **read-only transaction**, so a filter can never change
data — even one that calls a function with side effects.

Without a sort, rows are ordered by primary key so pages are stable.

## Inspecting a record

`enter` shows every column of the selected row with its type. JSON and JSONB
values are pretty-printed with coloured keys; long text wraps. Foreign keys
show where they point. `↑↓` scroll, `esc` goes back.

## Following relationships

Put the cursor on a foreign-key column (marked `↗` in the header) and press
`g`. Nexus opens the referenced table, filtered to the matching row, with a
breadcrumb showing how you got there (`users ← posts.author_id`). `esc`
returns.

## Editing

Press `e` on a cell, type the new value, `enter`, then `y` to save.
`ctrl+n` sets the cell to NULL.

How saving works:

- the update targets exactly one row **by primary key**, in a transaction;
  if it would touch any other number of rows, nothing is saved;
- your text is converted to the column's type by PostgreSQL itself, exactly
  as if you had typed it in SQL — dates, JSON, arrays and enums all work;
- constraint violations are shown in the status bar and nothing changes.

Editing is turned off:

- on **protected environments** (production), always;
- for tables **without a primary key** (rows can't be addressed safely);
- for **generated** and **identity always** columns, which PostgreSQL computes.

## Not interactive?

In a pipe or CI, `nexus browse users` prints the first page. For full
control use:

```bash
nexus table users rows --search ada --order "created_at desc" --limit 50
nexus table users rows --where "created_at > now() - interval '1 day'" --json
```

## The rest of `nexus table`

```bash
nexus table users              # structure: columns, indexes, relationships
nexus table users columns
nexus table users indexes      # with size and how often each is used
nexus table users relations    # foreign keys in both directions
nexus table users policies     # row-level security
```

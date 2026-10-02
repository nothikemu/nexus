# Making queries fast

Two tools: **`nexus query analyze`** for one query, **`nexus db inspect`**
for the whole schema.

## Analyze a query

```bash
nexus query analyze "select * from posts where title = 'post 4242'"
```

Nexus runs your query with `EXPLAIN ANALYZE` — **inside a transaction that
is always rolled back**, so even an `update` or `delete` leaves no trace —
and shows:

- the plan as a tree, with a bar for each step's share of the time
  (amber = sequential scan, green = index use),
- filters, join conditions and sort keys per step,
- rows scanned vs rows returned, cost, and cache hit ratio,
- findings, with a fix when there is one.

```
◈ query plan

  planning 362µs · execution 3.4ms

  Seq Scan on posts    ██████████    3.3ms   50,000 → 1 rows
     filter (title = 'post 4242'::text)
     49,999 rows removed by filter

  scanned 50,000 rows · returned 1 · cost 1141 · cache hit 100%

✦ nexus noticed something.

  ▲ posts has no index on title
    scanned 50,000 rows to return 1. An index on title lets PostgreSQL
    jump straight to matching rows.

      create index concurrently posts_title_idx on "public"."posts" ("title");
```

Without running the query — just the planner's estimate:

```bash
nexus db explain "select …"            # estimated
nexus db explain --analyze "select …"  # same as query analyze
```

### What Nexus looks for

| finding | means | suggested fix |
|---|---|---|
| no index on a filtered column | a large sequential scan threw most rows away | `create index concurrently …` |
| `lower(col) = …` with no expression index | a plain index can't help | an index on `lower(col)` |
| index exists but wasn't used | stale statistics, or the filter matches too much | `analyze table` |
| row estimate off by 10× or more | the planner is guessing | `analyze table` |
| sort spilled to disk | not enough `work_mem` | `set work_mem = …` |
| hash split into batches | not enough `work_mem` | `set work_mem = …` |

**Nexus never invents fixes.** Index suggestions are only made for columns
it has confirmed exist in the live catalog, and never when a usable index is
already there.

### Turning a suggestion into a migration

`create index concurrently` can't run in a transaction, so:

```sql
-- nexus:no-transaction
-- nexus:up
create index concurrently posts_title_idx on public.posts (title);

-- nexus:down
drop index concurrently public.posts_title_idx;
```

## Inspect the whole database

```bash
nexus db inspect
```

Shows size, objects, connections, cache hit ratio and uptime, then health
findings with fixes:

| check | |
|---|---|
| tables without a primary key | rows can't be addressed reliably |
| foreign keys without an index | joins and deletes on the parent scan the whole table |
| duplicate indexes | cost writes and space, add nothing |
| invalid indexes | usually a failed `create index concurrently` |
| tables needing vacuum | many dead rows |
| sequences near their maximum | inserts will start failing |
| large indexes never used | candidates to drop (check production first) |

`nexus doctor`, `nexus status` and the dashboard show the same findings in
short form.

## Housekeeping

```bash
nexus db analyze posts      # refresh planner statistics
nexus db vacuum posts       # reclaim dead rows (shows size before → after)
nexus db vacuum posts --full   # rewrite the table (locks it; asks first)
```

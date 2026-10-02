# The dashboard

Type `nexus` inside a project and the dashboard opens.

```bash
nexus
```

It refreshes every two seconds from your database's own statistics.

## Views

| key | view | shows |
|---|---|---|
| `1` | overview | database version, size, object counts, cache hit ratio, uptime · migrations · health · largest tables · live throughput sparklines (transactions, rows read, rows written, connections) · recent activity |
| `2` | tables | every table with rows, size and notes — `enter` opens the explorer |
| `3` | migrations | applied and pending migrations |
| `4` | activity | other sessions running queries right now: who, how long, what |

`tab` / `←→` also switch views, `↑↓` select, `r` refreshes, `q` quits.

## Nex on the dashboard

Nex sits in the top-left corner and reflects what's going on:

| Nex looks | because |
|---|---|
| ◕ω◕ calm | everything is fine |
| curious, one eye wide | there are pending migrations or health findings |
| asleep | the database isn't running |
| connecting | it's waking the database up |

## When the database is asleep

If the local database isn't running, the dashboard shows Nex asleep. Press
**`w`** to wake it — Nexus starts PostgreSQL right there and the dashboard
comes to life. (For remote environments you'll see why it can't connect
instead.)

## Not a terminal?

In a pipe or CI, `nexus` prints `nexus status` instead. Outside a project,
it shows a welcome screen with what to do next.

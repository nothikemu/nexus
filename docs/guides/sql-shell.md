# The SQL shell

```bash
nexus sql
```

```
▐◕ω◕▌  nexus sql · my_app · local · PostgreSQL 16.4
       \? help · \q quit · end statements with ;

my_app ❯ select email, name
       · from users
       · order by email;

  email               name
  ada@example.com     Ada Lovelace
  grace@example.com   Grace Hopper

  2 rows · 1.4ms
```

## Typing

| key | does |
|---|---|
| `enter` | runs the statement — once it ends with `;` (otherwise starts a new line) |
| `alt+enter` / `ctrl+j` | always adds a new line |
| `↑` / `↓` | previous / next statement from history |
| `ctrl+c` | cancels a running query · clears the line · quits when the line is empty |
| `ctrl+d` | quits |
| `ctrl+l` | clears the screen |

Nexus understands quoting, `$$` function bodies and comments, so a `;`
inside a string never runs your statement early. Meta commands (starting
with `\`) run straight away.

History is saved per project in `.nexus/sql_history` (or in your user
config folder when you're not in a project), and multi-line statements
come back intact.

## Meta commands

| command | does |
|---|---|
| `\dt` | list tables |
| `\d` | list tables and views |
| `\d users` | describe a table: columns, types, keys, indexes |
| `\dn` | list schemas |
| `\explain select …` | the plan for a query (estimated) |
| `\analyze select …` | run it and explain where the time went (rolled back) |
| `\x` | toggle expanded display (one record per block) |
| `\timing` | toggle timing |
| `\conninfo` | which database you're connected to |
| `\history` | recent statements |
| `\?` | help |
| `\q` | quit |

## Results

- Numbers are right-aligned, `null` is shown as a dimmed *null*, booleans as
  `true`/`false`, JSON compacted onto one line.
- Writes report in words: `updated 3 rows`, `inserted 1 row`.
- Very large results keep the first 500 rows and tell you so.
- Errors show PostgreSQL's message with a caret under the problem.

The shell keeps one connection for the whole session, so `begin;`,
`set …`, temporary tables and so on behave as you expect. If a statement
fails inside a transaction, Nexus reminds you to `rollback;`.

## One-off queries and scripts

You don't need the interactive shell for a quick query:

```bash
nexus sql "select count(*) from users"
nexus sql -f reports/weekly.sql            # errors show file line numbers
cat cleanup.sql | nexus sql
nexus sql "select * from users" --json     # typed JSON: numbers, booleans, nested JSON
nexus sql "select * from orders" -x        # expanded records
```

Statements run one at a time and each commits on its own, like `psql`.

## Safety on protected environments

On `--env production` (or any `protected: true` environment), statements
that write ask for confirmation, and destructive ones — `drop`, `truncate`,
`delete`, `update` without `where`, `alter … drop` — require typing
`DELETE PRODUCTION DATA`. Reads never ask.

## Prefer psql?

`nexus db shell` opens `psql` already connected to the right database (when
it's installed). `nexus db shell --builtin` opens the Nexus shell instead.

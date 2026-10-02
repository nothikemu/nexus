# Installing Nexus

Nexus is a single program called `nexus`. Installing it takes a minute.
Running a local database also needs **PostgreSQL** or **Docker** on your
machine — this page covers both.

- [1. Install the `nexus` command](#1-install-the-nexus-command)
- [2. Give Nexus a database to run](#2-give-nexus-a-database-to-run)
- [3. Check everything works](#3-check-everything-works)
- [Shell completion](#shell-completion)
- [Upgrading and uninstalling](#upgrading-and-uninstalling)

## 1. Install the `nexus` command

### The quick way (macOS, Linux, WSL)

```bash
curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | sh
```

The script:

1. detects your OS and CPU,
2. downloads the matching release from GitHub and **checks its SHA-256
   checksum** (it refuses to install on a mismatch),
3. puts `nexus` in `/usr/local/bin` if that's writable, otherwise
   `~/.local/bin`,
4. tells you if that folder isn't on your `PATH`, and whether PostgreSQL or
   Docker is missing.

If there is no prebuilt release for your platform yet, and Go is installed,
it builds Nexus from source instead.

Options:

```bash
# a specific version
curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | NEXUS_VERSION=v0.2.0 sh
# somewhere else
curl -fsSL https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh | NEXUS_INSTALL_DIR=~/bin sh
```

Prefer to read scripts before running them? Download it first:
`curl -fsSLO https://raw.githubusercontent.com/nothikemu/nexus/main/install.sh`, read it, then `sh install.sh`.

### With Go

If you have Go 1.24 or newer:

```bash
go install github.com/nothikemu/nexus/cmd/nexus@latest
```

This puts `nexus` in `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure
that folder is on your `PATH`.

### Download a release by hand

Every tagged release on the
[releases page](https://github.com/nothikemu/nexus/releases) has archives for
Linux, macOS and Windows on both Intel/AMD (`amd64`) and ARM (`arm64`), plus
`checksums.txt`. Unpack the archive and move `nexus` (or `nexus.exe`) onto
your `PATH`.

> **Windows:** Nexus builds and runs on Windows, and the native and Docker
> runtimes are written for it, but the most tested setup is Linux/macOS (or
> WSL on Windows).

### From source

```bash
git clone https://github.com/nothikemu/nexus
cd nexus
make build          # creates ./bin/nexus
make install        # or install into your Go bin folder
```

## 2. Give Nexus a database to run

`nexus up` starts a PostgreSQL database for your project. It can do that
three ways, and picks automatically (`database.runtime: auto`):

| you have | Nexus uses | |
|---|---|---|
| PostgreSQL installed | **native** | fastest; the database lives in your project's `.nexus/` folder |
| Docker running | **docker** | one container per project, data in a named volume |
| a database already (cloud, server) | **external** | set `database.url` — Nexus never starts or stops it |

### Installing PostgreSQL

| system | command |
|---|---|
| macOS (Homebrew) | `brew install postgresql@16` |
| macOS (app) | [Postgres.app](https://postgresapp.com) — Nexus finds it automatically |
| Ubuntu / Debian | `sudo apt install postgresql` |
| Fedora / RHEL | `sudo dnf install postgresql-server` |
| Arch | `sudo pacman -S postgresql` |
| Windows | the installer from [postgresql.org](https://www.postgresql.org/download/windows/) |

You don't need to start or configure the system PostgreSQL service — Nexus
only uses the programs (`initdb`, `pg_ctl`, `postgres`) and runs its own,
private database per project. If Nexus can't find them, point it at the
folder: `export NEXUS_PG_BIN=/path/to/postgresql/bin`.

Supported versions: PostgreSQL 13 through 18 (16 by default).

### Using Docker instead

Install [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
Docker Engine, make sure it's running, and either let `auto` pick it or set
it explicitly:

```bash
nexus init my-app --runtime docker
```

> Running as `root` (for example inside a container)? PostgreSQL refuses to
> run as root, so Nexus picks Docker automatically there.

## 3. Check everything works

```bash
nexus version
nexus doctor
```

`nexus doctor` checks your setup — PostgreSQL, Docker, configuration,
connection, migrations, secrets, terminal — and tells you exactly what to fix.

Then say hi:

```bash
nexus hi
```

Next: **[Getting started](GETTING-STARTED.md)** — your first project in five
minutes.

## Shell completion

Nexus completes commands, flags — and even your table names.

```bash
# bash
nexus completion bash > ~/.local/share/bash-completion/completions/nexus
# zsh
nexus completion zsh > "${fpath[1]}/_nexus"
# fish
nexus completion fish > ~/.config/fish/completions/nexus.fish
# PowerShell
nexus completion powershell | Out-String | Invoke-Expression
```

Open a new shell afterwards.

## Upgrading and uninstalling

**Upgrade:** run the install script again (or `go install …@latest`). Your
projects and their data are untouched.

**Uninstall:**

```bash
rm "$(command -v nexus)"
rm -rf ~/.config/nexus          # Nex's memory and global SQL history (macOS: ~/Library/Application\ Support/nexus)
```

Each project keeps its local database in its own `.nexus/` folder. Stop it
with `nexus down` and delete that folder to remove it (or `nexus dev reset`
to start fresh).

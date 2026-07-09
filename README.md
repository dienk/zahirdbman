# zahirdbman

A lightweight, self-contained **PostgreSQL database manager** with a web UI,
written in Go. Browse databases, schemas, tables and rows; run ad-hoc SQL; and
create or drop databases — all from the browser. The server compiles to a
single binary with the UI (templates + CSS) embedded.

## Features

- **Databases overview** — list every database with owner and on-disk size.
- **Table browser** — drill into a database to see its tables/views, estimated
  row counts and sizes; open a table to inspect columns and preview rows.
- **SQL console** — run arbitrary SQL against a selected database and view
  results in a table; non-SELECT statements report their command tag.
- **Create / drop databases** — with a confirmation guard and automatic
  termination of open sessions before a drop.
- **Backup & Restore** — download a `pg_dump` of any database (custom `.dump`
  or plain `.sql`), and restore an uploaded dump into a target database, with
  options to create the target first, clean existing objects, or ignore
  ownership. Requires the `pg_dump` / `pg_restore` / `psql` client tools on the
  server's `PATH` (bundled in the Docker image).
- **Self-contained** — templates and static assets are embedded via `go:embed`.
- **Safe by construction** — identifiers are validated and quoted; parameters
  use bound values.

## Requirements

- Go 1.23+
- A reachable PostgreSQL server

## Configuration

Configuration is read from environment variables (see `.env.example`):

| Variable        | Default     | Description                                  |
|-----------------|-------------|----------------------------------------------|
| `ZDBM_ADDR`     | `:8080`     | HTTP listen address                          |
| `PGHOST`        | `localhost` | PostgreSQL host                              |
| `PGPORT`        | `5432`      | PostgreSQL port                              |
| `PGUSER`        | `postgres`  | Connection user                              |
| `PGPASSWORD`    | *(empty)*   | Connection password                          |
| `PGSSLMODE`     | `prefer`    | libpq SSL mode                               |
| `ZDBM_ADMIN_DB` | `postgres`  | Maintenance DB for server-wide operations    |

## Running

```sh
# using the Makefile
make run

# or directly
go run ./cmd/server
```

Then open <http://localhost:8080>.

To build a release binary:

```sh
make build      # produces bin/zahirdbman
./bin/zahirdbman
```

## Docker

The app builds to a static binary and ships in a minimal distroless image.

**Whole stack (app + PostgreSQL) with Compose:**

```sh
docker compose up --build      # or: make up
# open http://localhost:8080
docker compose down            # or: make down  (add -v to wipe the volume)
```

Compose starts PostgreSQL, waits until it is healthy (`pg_isready`), then
starts zahirdbman pointed at it (`PGHOST=db`, `PGSSLMODE=disable`). Override
`PGUSER` / `PGPASSWORD` / `ZDBM_ADMIN_DB` via a `.env` file or the shell.

**Just the app image, against an existing database:**

```sh
make docker-build                          # builds zahirdbman:latest
docker run --rm -p 8080:8080 \
  -e PGHOST=host.docker.internal \
  -e PGUSER=postgres -e PGPASSWORD=secret \
  zahirdbman:latest
```

The image runs as a non-root user and exposes port `8080`.

## Deploy to Render

This repo includes a [`render.yaml`](render.yaml) blueprint that provisions a
managed PostgreSQL database and a Docker web service, wired together
automatically.

1. Push this repo to GitHub.
2. In the [Render dashboard](https://dashboard.render.com): **New → Blueprint**,
   then select this repository. Render reads `render.yaml` and creates both
   resources.
3. Open the web service URL when the deploy finishes.

How it works:

- The web service builds from the [`Dockerfile`](Dockerfile) (Alpine image with
  the `psql` client tools, so Backup & Restore works in the cloud too).
- The app listens on Render's `PORT` and reads `DATABASE_URL` from the managed
  database — no manual env wiring.
- `GET /livez` is the liveness probe (process up); `GET /healthz` additionally
  checks the database.

Notes:

- **Free tier** databases expire after 30 days and free web services sleep when
  idle — fine for trials, not production.
- The managed database's role is not a superuser, so the built-in
  create/drop/list-all-databases actions are limited to that one database. To
  manage other servers, add them under **Connections** in the UI.
- Connection profiles live in a JSON file. To persist user-added profiles
  across deploys, switch to a paid instance and uncomment the disk block in
  `render.yaml`.

The same image runs on any Docker host (Fly.io, Railway, a VPS); set `PORT`
and/or `DATABASE_URL` (or the individual `PG*` variables) accordingly.

## Project layout

```
zahirdbman/
├── assets.go              # go:embed of the web/ assets
├── cmd/server/main.go     # entrypoint, config, graceful shutdown
├── internal/
│   ├── config/            # env-based configuration + DSN builder
│   ├── store/             # PostgreSQL access layer (pgx)
│   └── handler/           # HTTP routes + HTML rendering
└── web/
    ├── templates/         # html/template pages
    └── static/            # app.css
```

## Health check

`GET /healthz` pings the admin database and returns `ok` (200) or an error (503).

## Security notes

zahirdbman executes SQL you type and can drop databases. Run it only against
servers you control, ideally behind authentication / a private network. It does
not add its own auth layer.

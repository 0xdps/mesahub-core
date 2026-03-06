# sqlite-hub

sqlite-hub is a centralized SQLite platform for internal workloads.

It gives each service its own database file, keeps everything on one persistent volume, and provides a secure web UI for browsing/querying data.

## Why sqlite-hub

- One place to manage all internal SQLite databases
- Built-in admin dashboard and SQL viewer
- Simple token-based admin auth
- Railway-friendly deployment model with persistent storage
- Minimal operational overhead

## What you get

- Database registry and metadata (`owner`, `description`, status)
- Health and metrics endpoints
- Read/query APIs per database
- Volume-aware guardrails for DB creation
- Studio UI for table browsing and query execution

## Quick start

```bash
git clone https://github.com/0xdps/sqlite-hub.git
cd sqlite-hub
npm install
cp .env.example .env
just doctor
just dev
```

App URL: [http://sqlite-hub.localhost:1355](http://sqlite-hub.localhost:1355)

## Runtime model

- Single compose file: `docker-compose.yml`
- Dev profile: `app-dev` (Docker random port + Portless alias)
- Prod profile: `app-prod` (fixed local port, platform `$PORT` in deployment)
- Shared bootstrap script: `start.sh`

Common commands:

```bash
just doctor      # preflight checks
just dev         # foreground dev with logs
just prod        # local production-like mode
just smoke       # health + login checks against running service
just down        # stop running services
just dev-clean   # remove stale containers/orphans
```

## Railway deployment

1. Create a service from this repository.
2. Attach a persistent volume mounted at `/data`.
3. Set required environment variables:
	- `ADMIN_TOKEN`
	- `SESSION_SECRET`
4. Deploy using Dockerfile build (`railway.json`).

## API overview

Databases:

- `GET /api/db` list active databases
- `POST /api/db` create/register a database
- `GET /api/db/:name` fetch metadata for one database
- `DELETE /api/db/:name` soft-delete a database

Queries:

- `POST /api/db/:name/query` execute read-only SQL

System:

- `GET /api/health` service health
- `GET /api/metrics` storage and request metrics

All API routes except `/api/health` require admin authentication.

## Documentation

- [docs/DEVELOPMENT.md](./docs/DEVELOPMENT.md) - full development and operations guide
- [docs/PRD.md](./docs/PRD.md) - product requirements
- [docs/TECH.md](./docs/TECH.md) - implementation reference

## License

MIT

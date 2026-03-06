# DEVELOPMENT.md

This guide explains local development, runtime behavior, deployment expectations, and troubleshooting for sqlite-hub.

## 1. Development principles

- Keep one runtime model for both dev and prod.
- Keep differences explicit and minimal.
- Prefer just recipes as the single operator interface.
- Keep Docker profile contract stable (`app-dev`, `app-prod`).

## 2. Runtime architecture

sqlite-hub uses a profile-based compose setup.

- Dev profile (`app-dev`):
  - Docker maps container `:80` to a random host port.
  - Portless alias `sqlite-hub.localhost:1355` points to the mapped host port.
  - Logs stream in foreground via `just dev`.
- Prod profile (`app-prod`):
  - Host port is fixed (`PORT`, default `8080`).
  - Uses the same bootstrap script and app flow.

Both profiles use:

- `docker-compose.yml` as single compose source
- `start.sh` as shared bootstrap entrypoint
- `/data` for SQLite files and storage metadata

## 3. Prerequisites

- Docker + Docker daemon running
- Node.js + npm
- just
- Portless available via `npx portless`

Check everything:

```bash
just doctor
```

## 4. Environment setup

Create local env file:

```bash
cp .env.example .env
```

Minimum required values:

- `ADMIN_TOKEN`
- `SESSION_SECRET`

Commonly used values:

- `DATA_PATH=/data`
- `MAX_VOLUME_USAGE_PERCENT=85`
- `ENABLE_FILE_STORAGE=true`
- `ENABLE_FILE_PROXY_DELIVERY=true`

## 5. Local workflows

### 5.1 Development mode

```bash
just dev
```

What happens:

1. Portless proxy is started (or reused).
2. Dev service starts with compose profile `dev`.
3. Mapped host port is detected automatically.
4. Alias `sqlite-hub.localhost` is pointed to that port.
5. Health check polling confirms startup readiness.
6. Logs stream in foreground.

Access URL:

- `http://sqlite-hub.localhost:1355`

Stopping:

- Press `Ctrl+C` to stop the foreground dev session.
- Run `just down` to ensure all services are stopped.

### 5.2 Production-like mode (local)

```bash
just prod
```

Default URL:

- `http://localhost:8080`

Override port:

```bash
just prod port=9090
```

### 5.3 Health/smoke checks

Run smoke checks against the running dev/prod service:

```bash
just smoke
```

Current checks:

- `GET /api/health` returns `200`
- `POST /api/auth/login` with `ADMIN_TOKEN` returns `200`

## 6. Build and validation

Build images:

```bash
just build-dev
just build-prod
```

Validation:

```bash
just contract   # compose profile contract validation
just check      # typecheck + build
```

## 7. Operational recipes

Status and ports:

```bash
just status
just show-ports
```

Logs:

```bash
just logs
just logs service=app-prod
```

Cleanup:

```bash
just down
just dev-clean
```

`dev-clean` removes old/stale containers, orphans, and alias leftovers.

## 8. Deployment notes (Railway)

- Build uses Dockerfile via `railway.json`.
- Attach persistent volume at `/data`.
- Set `ADMIN_TOKEN` and `SESSION_SECRET` in Railway env.
- Keep startup path unified via Dockerfile CMD + `start.sh`.

## 9. Troubleshooting

### 9.1 `just doctor` fails

- Docker daemon not running: start Docker Desktop.
- `.env` missing: run `cp .env.example .env`.
- Portless unavailable: verify `npx portless --help` works.

### 9.2 `just dev` does not show URL

- Port mapping may not be ready yet; wait a few seconds.
- Check container state with `just status`.
- Check logs with `just logs`.
- Retry with `just dev-clean` then `just dev`.

### 9.3 `just smoke` fails

- Ensure dev or prod service is running.
- Verify `ADMIN_TOKEN` exists in `.env`.
- Validate health endpoint manually:
  - `curl http://localhost:<mapped-port>/api/health`

### 9.4 stale container names or old mappings

Run:

```bash
just dev-clean
```

Then restart:

```bash
just dev
```

## 10. CI contract

Current CI check flow includes:

1. install dependencies
2. prepare `.env`
3. run `just contract`
4. run `just check`

This protects dev/prod profile contract and build integrity.

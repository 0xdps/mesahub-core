# sqlite-hub Setup Guide

This guide covers everything you need to go from zero to a running sqlite-hub instance — locally or on Railway.

---

## What sqlite-hub gives you

- **A database per service** — each app gets its own SQLite file, its own bearer token, and its own storage quota
- **REST API** — query and write SQL over HTTP from any language, no driver needed
- **File storage** — upload and download files scoped to a database, with presigned URL support
- **Admin dashboard** — a web UI to browse tables, run queries, and manage databases
- **Soft deletes** — deleted databases are recoverable for 30 days

---

## Prerequisites

| Tool | Required for |
|---|---|
| Docker + Docker Compose | Local development |
| [just](https://github.com/casey/just) | Running project commands |
| openssl | Generating secrets |

---

## Local setup

### 1. Clone and install

```bash
git clone https://github.com/0xdps/sqlite-hub-template.git my-sqlite-hub
cd my-sqlite-hub
npm install
```

### 2. Configure environment

```bash
cp .env.example .env
```

Open `.env` and fill in the two required values:

```bash
ADMIN_TOKEN=<openssl rand -hex 32>
SESSION_SECRET=<openssl rand -hex 32>
```

Everything else already has a working default.

### 3. Start

```bash
just dev        # starts with hot-reload, logs in foreground
```

The dashboard is at **http://sqlite-hub.localhost:1355** (requires [Portless](https://portless.dev) for the `.localhost` alias, or use `http://localhost:1355` without it).

Sign in with your `ADMIN_TOKEN`.

### Useful commands

```bash
just doctor      # checks Docker, ports, env, and dependencies
just dev         # foreground dev with logs
just down        # stop all containers
just dev-clean   # remove stale containers and orphaned volumes
just smoke       # health + login checks against a running service
just check       # typecheck + build (runs in CI)
```

---

## Railway deployment

### 1. Fork or push to your own repo

Railway deploys from a git repository. Fork this repo or push to a new one under your account.

### 2. Create a Railway service

In the Railway dashboard, create a new **Empty Project**, then add a service from your repository. Railway detects `railway.json` and uses the Dockerfile automatically — no extra config needed.

### 3. Attach a persistent volume

Go to the service → **Volumes** → add a volume mounted at `/data`.

> Without a volume every deploy wipes your databases, registry, and blobs.

### 4. Set environment variables

Only two variables are required. Generate them and paste into Railway's service variables UI:

```bash
for v in ADMIN_TOKEN SESSION_SECRET; do
  echo "$v=$(openssl rand -hex 32)"
done
```

| Variable | Required | Default | Notes |
|---|---|---|---|
| `ADMIN_TOKEN` | **yes** | — | Admin dashboard + API auth |
| `SESSION_SECRET` | **yes** | — | Min 32 chars |
| `NODE_ENV` | no | `production` | Leave as-is |
| `DATA_PATH` | no | `/data` | Must match volume mount |
| `HOSTNAME` | no | `::` | Required for Railway IPv6 networking |
| `MAX_VOLUME_USAGE_PERCENT` | no | `85` | DB creation blocked above this % |
| `ENABLE_FILE_STORAGE` | no | `true` | Disable to turn off file upload support |
| `FILE_MAX_SIZE_BYTES` | no | `104857600` | 100 MB per file |
| `FILE_MAX_STORAGE_PER_DB_BYTES` | no | `5368709120` | 5 GB per database |
| `FILE_URL_SIGNING_SECRET` | no | falls back to `ADMIN_TOKEN` | Set in production to isolate signing keys |
| `FILE_TOKEN_SIGNING_SECRET` | no | falls back to `ADMIN_TOKEN` | Set in production to isolate signing keys |

### 5. Deploy

Trigger a deploy (or push to your branch). Railway will:

1. Build the multi-stage Dockerfile — Go binary, Next.js standalone, Caddy reverse proxy
2. Start via `/app/start.sh`
3. Health-check `GET /api/health` with a 120s timeout
4. Route traffic once healthy

Sign in at your Railway service URL using your `ADMIN_TOKEN`.

---

## Creating your first database

### Via the dashboard

1. Open the dashboard and sign in
2. Click **New database**
3. Give it a name (lowercase `a-z`, `0-9`, `-`, `_`) and an optional description

### Via the API

Use the admin token as a Bearer header:

```bash
curl -X POST https://your-service.railway.app/api/db \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-app", "owner": "my-team"}'
```

Response:

```json
{
  "id": 1,
  "name": "my-app",
  "owner": "my-team",
  "status": "active",
  "created_at": "2026-04-03T10:00:00Z"
}
```

---

## Querying a database

All query and exec endpoints accept either an admin bearer token or a scoped API key (`shs_...`).

### Read — POST /api/db/:name/query

```bash
curl -X POST https://your-service.railway.app/api/db/my-app/query \
  -H "Authorization: Bearer shs_abc123..." \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, email FROM users WHERE active = ?", "bindings": [1]}'
```

Response:

```json
{
  "rows": [
    {"id": 1, "email": "jane@example.com"},
    {"id": 2, "email": "wade@example.com"}
  ],
  "stat": {"rowsRead": 2, "queryDurationMs": 2}
}
```

### Write — POST /api/db/:name/exec

```bash
curl -X POST https://your-service.railway.app/api/db/my-app/exec \
  -H "Authorization: Bearer shs_abc123..." \
  -H "Content-Type: application/json" \
  -d '{"sql": "INSERT INTO events (type, user_id) VALUES (?, ?)", "bindings": ["signup", 42]}'
```

Response:

```json
{
  "rowsAffected": 1,
  "lastInsertRowid": 99
}
```

> `/query` only accepts SELECT-class statements. Writes must go through `/exec`.

---

## File storage

Each database has an independent file store. Files are stored in `/data/files/blobs` on the volume and exposed through the database bearer token.

### Upload

```bash
curl -X POST https://your-service.railway.app/api/db/my-app/files \
  -H "Authorization: Bearer shs_abc123..." \
  -F "file=@photo.jpg" \
  -F "folder_path=uploads/photos" \
  -F "filename=photo.jpg"
```

### List

```bash
curl https://your-service.railway.app/api/db/my-app/files \
  -H "Authorization: Bearer shs_abc123..."
```

### Presigned URL (browser-safe, no credentials)

```bash
curl -X POST https://your-service.railway.app/api/db/my-app/files/:id/presign \
  -H "Authorization: Bearer shs_abc123..." \
  -H "Content-Type: application/json" \
  -d '{"ttl_seconds": 900, "disposition": "inline"}'
```

Returns a signed URL valid for 15 minutes with no auth header needed.

---

## Authentication summary

| Caller | Token type | Where to use |
|---|---|---|
| Admin (you) | `ADMIN_TOKEN` | Dashboard login, `POST /api/db`, `GET /api/db` |
| Your app / services | `shs_...` (per-database) | All `/api/db/:name/query`, `/exec`, `/files` routes |
| Browser file downloads | Presigned URL | No token needed — URL carries a time-limited HMAC signature |

---

## Health and metrics

```bash
# Public — no auth required
curl https://your-service.railway.app/api/health

# Admin — requires bearer token
curl https://your-service.railway.app/api/metrics \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

---

## Soft deletes and restore

Deleting a database via the dashboard or API is non-destructive for 30 days:

```bash
# Soft delete
curl -X DELETE https://your-service.railway.app/api/db/my-app \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# List deleted databases
curl https://your-service.railway.app/api/db/deleted \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Restore
curl -X POST https://your-service.railway.app/api/db/deleted/my-app \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action": "restore"}'
```

After 30 days (or on `hard_delete`) the file is permanently removed from the volume.

---

## Control plane integration

If you are pairing this template with the sqlite-hub control plane service, enable the integration:

```bash
ENABLE_CONTROL_DB=true
CONTROL_DB_SECRET=<openssl rand -hex 32>
```

Set the same `CONTROL_DB_SECRET` in the control plane's environment. On startup the template will auto-provision a database named `control` with that secret, which the control plane uses to store user accounts and API key metadata.

For cache setup in control mode:

```bash
# Single instance (recommended to start)
CACHE_MODE=local

# Later, for multi-instance consistency
# CACHE_MODE=redis
# REDIS_URL=redis://<host>:6379/0
```

`CACHE_MODE=off` is also supported if you want to disable caching entirely.

---

## Security checklist before going live

- [ ] `ADMIN_TOKEN` is a strong random value (`openssl rand -hex 32`)
- [ ] `SESSION_SECRET` is at least 32 characters, unique to this deployment
- [ ] Volume is attached at `/data` — data survives deploys
- [ ] Each application service has its own scoped API key (`shs_...`), not the admin token
- [ ] `FILE_URL_SIGNING_SECRET` and `FILE_TOKEN_SIGNING_SECRET` set separately from `ADMIN_TOKEN` in production

# file-db

**A centralized SQLite storage service for internal Railway workloads.**

---

## 1. Problem

Internal services that need lightweight persistent storage end up creating scattered SQLite instances with no visibility, no uniform backup strategy, and no way to see what data lives where.

---

## 2. Solution

A single Railway service with one persistent volume that acts as the home for all internal SQLite databases. Services mount the same volume and connect to their own `.db` file directly.

The UI is built on top of **Outerbase Studio** (open-source, AGPL-3.0) — its cloud/workspace/telemetry layers are stripped out and replaced with our own admin pages and a custom SQLite driver that proxies queries through our Next.js API routes. The result is one Next.js app: a DB registry dashboard and a full-featured SQLite viewer, all behind a single login.

---

## 3. Goals

- All `.db` files live under a single Railway volume at `/data`
- Railway's native volume backup covers everything automatically
- Admin UI shows ownership, file size, and volume usage at a glance
- DB contents are viewable via the Outerbase Studio UI (table browser, query editor, schema viewer)
- Everything is one Next.js app — single URL, single login, no sidecars
- Telemetry and cloud features fully removed from the fork
- Operational overhead stays near zero

---

## 4. Non-Goals

- Not a SQL proxy or query-over-HTTP service
- Not horizontally scalable
- Not a PostgreSQL replacement
- No inter-database communication
- No fine-grained per-service access control (internal use only)

---

## 5. Architecture

### 5.1 Infrastructure

| Concern | Approach                                    |
| ------- | ------------------------------------------- |
| Hosting | Railway — single service                    |
| Storage | Single persistent volume mounted at `/data` |
| Backup  | Railway volume snapshots (daily + weekly)   |
| Scaling | Single instance only                        |
| Runtime | Single Next.js process — no sidecars        |

### 5.2 Volume Layout

```
/data
  registry.db        ← metadata store
  service-a.db
  service-b.db
  analytics.db
  ...
```

### 5.3 Request Flow

```
Internet
  └── Next.js :3000 (public)
        ├── middleware.ts → auth check on all routes
        │     └── unauthenticated → redirect to /login
        ├── /api/* → Next.js API routes (registry CRUD + query proxy)
        ├── /db/[name] → Outerbase Studio <Studio /> component
        └── / → Admin dashboard (DB list, storage stats)
```

### 5.4 How Services Connect

Each internal service receives the Railway volume mount and opens its assigned `.db` file directly via the filesystem path (e.g. `/data/service-a.db`). There is no network layer or proxy — one service, one DB, no contention.

---

## 6. Components

### 6.1 Registry (`registry.db`)

The control plane metadata store. Tracks all known databases.

```sql
CREATE TABLE databases (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT UNIQUE NOT NULL,   -- maps to /data/{name}.db
  owner       TEXT NOT NULL,          -- service or team name
  description TEXT,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  status      TEXT DEFAULT 'active'   -- active | deleted
);
```

### 6.2 Admin API

A minimal HTTP API for managing the registry. Protected by `ADMIN_TOKEN`.

| Method | Path      | Description                                |
| ------ | --------- | ------------------------------------------ |
| GET    | /health   | Liveness check                             |
| GET    | /metrics  | Volume usage summary (total, per-DB sizes) |
| GET    | /db       | List all registered databases + file stats |
| POST   | /db       | Register and create a new database         |
| GET    | /db/:name | Get metadata and file stats for one DB     |
| DELETE | /db/:name | Soft-delete a database (marks inactive)    |

**`POST /db` behavior:**

1. Validate `name` matches `^[a-z0-9_-]+$`
2. Reject if disk usage exceeds `MAX_VOLUME_USAGE_PERCENT`
3. Reject if name already exists in registry
4. Create `/data/{name}.db` and enable WAL mode
5. Insert record into `registry.db`

### 6.3 Auth (Next.js middleware)

Next.js `middleware.ts` runs on every request. Checks for a valid signed session cookie. Unauthenticated requests redirect to `/login`.

| Route              | Description                                               |
| ------------------ | --------------------------------------------------------- |
| `/login`           | Login form — validates `ADMIN_TOKEN`, sets session cookie |
| `/api/auth/logout` | Clears session cookie                                     |

### 6.4 Admin UI (Next.js pages)

- `/` — Registry dashboard: all DBs with owner, description, size, created date, status; volume usage indicator
- `/db/new` — Form to register a new database
- `/db/[name]` — Renders the Outerbase Studio `<Studio />` component wired to the custom `FildbDriver` for that DB

### 6.5 DB Viewer (Outerbase Studio — forked)

The [Outerbase Studio](https://github.com/outerbase/studio) open-source UI is forked as the base of this project. The following is removed from the fork:

- All cloud/workspace/team pages (`(dark-only)`, `(outerbase)/w/`)
- All non-SQLite drivers (Turso, Cloudflare D1, rqlite, etc.)
- Telemetry module (`lib/tracking.ts`, `api/events/`)
- Outerbase cloud API client (`outerbase-cloud/`)

A custom `FildbDriver` is added that implements the `QueryableBaseDriver` interface by calling `POST /api/db/[name]/query` — our own Next.js API route that runs the query via `better-sqlite3` on the server.

---

## 7. Safety

| Concern             | Mitigation                                                              |
| ------------------- | ----------------------------------------------------------------------- |
| Path traversal      | Name validated with regex; path always constructed as `/data/{name}.db` |
| Disk exhaustion     | Reject new DB creation when volume usage > `MAX_VOLUME_USAGE_PERCENT`   |
| Accidental loss     | Soft deletes only; no immediate file removal                            |
| Unauthorized access | `ADMIN_TOKEN` required for all admin endpoints and dashboard            |

---

## 8. Configuration

```env
DATA_PATH=/data
ADMIN_TOKEN=<secret>
SESSION_SECRET=<secret>
MAX_VOLUME_USAGE_PERCENT=85
```

---

## 9. Backup & Restore

- **Backup**: Railway volume snapshots on a daily + weekly schedule. No custom logic needed.
- **Restore**: Restore volume snapshot via Railway dashboard → redeploy service → all state recovered.

---

## 10. Tech Stack

| Layer         | Choice                                           |
| ------------- | ------------------------------------------------ |
| Framework     | Next.js 15 (App Router)                          |
| UI base       | Outerbase Studio (forked, stripped, AGPL-3.0)    |
| DB access     | better-sqlite3 (server-side only)                |
| Custom driver | `FildbDriver` — implements `QueryableBaseDriver` |
| Auth          | Signed session cookie (Next.js middleware)       |
| Styling       | Tailwind CSS (already in Outerbase Studio)       |
| Deploy        | Railway (Dockerfile)                             |
| Processes     | 1 — Next.js only                                 |

---

## 11. Out of Scope (for now)

- Per-DB API keys or per-service auth
- Per-DB storage quotas
- Audit logs
- S3 / external export
- Multi-environment (dev/prod) separation
- Automated archival or TTL on soft-deleted files

---

## 12. Acceptance Criteria

- [ ] Volume mounted at `/data` and persists across redeploys
- [ ] `registry.db` auto-initialized on first boot
- [ ] `POST /db` creates a `.db` file and registry entry
- [ ] Admin UI lists all DBs with size and owner
- [ ] Login page issues session cookie; unauthenticated requests redirect to `/login`
- [ ] Admin dashboard lists all DBs with owner, size, and status
- [ ] `/db/[name]` opens Outerbase Studio UI for that specific DB
- [ ] Telemetry fully removed — no outbound requests to outerbase.com
- [ ] Disk limit enforcement blocks creation when volume is > 85% full
- [ ] No path traversal possible via DB name input
- [ ] Railway volume backup confirmed active

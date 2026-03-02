# TECH.md — file-db Implementation Reference

---

## 1. Project Structure

```
file-db/
├── Dockerfile
├── Caddyfile
├── supervisord.conf
├── package.json
├── tsconfig.json
├── src/
│   ├── index.ts           ← Hono entry point, mounts all routes
│   ├── db/
│   │   └── registry.ts    ← registry.db init + queries
│   ├── routes/
│   │   ├── auth.ts        ← /login, /logout, /auth/verify
│   │   ├── databases.ts   ← /api/db CRUD
│   │   └── system.ts      ← /api/health, /api/metrics
│   ├── middleware/
│   │   └── session.ts     ← cookie session middleware
│   ├── lib/
│   │   ├── fs.ts          ← disk usage, file creation helpers
│   │   └── outerbase.ts   ← Outerbase API client
│   └── views/
│       ├── layout.eta
│       ├── login.eta
│       ├── index.eta      ← DB list dashboard
│       └── new-db.eta     ← register new DB form
└── .env.example
```

---

## 2. Process Architecture

Three processes run inside a single container, managed by **supervisord**:

| Process    | Port  | Visibility         |
|------------|-------|--------------------|
| Caddy      | 80    | Public             |
| Hono       | 3000  | localhost only     |
| Outerbase  | 3001  | localhost only     |

Caddy is the only externally exposed port.

---

## 3. Caddy Configuration (`Caddyfile`)

```caddy
:80 {
  # Auth gate — every request hits this first
  forward_auth localhost:3000 {
    uri /auth/verify
    copy_headers Cookie
  }

  # API routes go to Hono
  handle /api/* {
    reverse_proxy localhost:3000
  }

  # Login/logout pages go to Hono (exempt from forward_auth above
  # because Hono's /auth/verify returns 200 for these paths)
  handle /login {
    reverse_proxy localhost:3000
  }

  handle /logout {
    reverse_proxy localhost:3000
  }

  # Everything else goes to Outerbase
  handle {
    reverse_proxy localhost:3001
  }
}
```

> **Note:** `/login` and `/logout` must be excluded from the auth gate at the Hono level — `/auth/verify` should return `200` for unauthenticated requests to those paths so the login form is always reachable.

---

## 4. Supervisord Configuration (`supervisord.conf`)

```ini
[supervisord]
nodaemon=true
logfile=/dev/null
logfile_maxbytes=0

[program:caddy]
command=caddy run --config /app/Caddyfile
autostart=true
autorestart=true
stdout_logfile=/dev/fd/1
stderr_logfile=/dev/fd/2

[program:hono]
command=node dist/index.js
directory=/app
autostart=true
autorestart=true
stdout_logfile=/dev/fd/1
stderr_logfile=/dev/fd/2

[program:outerbase]
command=<outerbase start command>
autostart=true
autorestart=true
stdout_logfile=/dev/fd/1
stderr_logfile=/dev/fd/2
```

---

## 5. Dockerfile

```dockerfile
FROM node:22-slim AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-slim
WORKDIR /app

# Install Caddy
RUN apt-get update && apt-get install -y caddy supervisor curl && rm -rf /var/lib/apt/lists/*

# Install Outerbase (update once self-hosted install method is confirmed)
# RUN <outerbase install command>

COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json .
COPY Caddyfile /app/Caddyfile
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

EXPOSE 80

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
```

---

## 6. Auth Flow

```
1. User hits any URL
2. Caddy calls Hono GET /auth/verify with Cookie header
3. Hono checks for valid signed session cookie
   - Valid   → 200 → Caddy proxies request to Hono or Outerbase
   - Invalid → 401 → Caddy redirects to /login
4. User submits /login with ADMIN_TOKEN value
5. Hono validates token, sets signed HttpOnly cookie (SESSION_SECRET)
6. User is redirected to /
```

### Session Cookie

- Name: `filedb_session`
- HttpOnly: true
- SameSite: Lax
- Signed with `SESSION_SECRET` (HMAC)
- No expiry set (browser session) — can add `Max-Age` if desired

---

## 7. Registry DB (`registry.db`)

Initialized on startup if it doesn't exist.

```typescript
// src/db/registry.ts
import Database from 'better-sqlite3'
import path from 'path'

const DATA_PATH = process.env.DATA_PATH ?? '/data'

export const registry = new Database(path.join(DATA_PATH, 'registry.db'))

registry.exec(`
  CREATE TABLE IF NOT EXISTS databases (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT UNIQUE NOT NULL,
    owner       TEXT NOT NULL,
    description TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    status      TEXT DEFAULT 'active'
  );
`)

// Enable WAL on registry itself
registry.pragma('journal_mode = WAL')
```

---

## 8. API Routes

### `POST /api/db`

```
Body: { name: string, owner: string, description?: string }

1. Validate name: /^[a-z0-9_-]+$/
2. Check disk usage < MAX_VOLUME_USAGE_PERCENT
3. Check name not already in registry
4. Create /data/{name}.db, PRAGMA journal_mode=WAL
5. INSERT into registry.db
6. Call Outerbase API to register new data source
7. Return 201 + created record
```

### `GET /api/db`

Returns all registry records + real-time file size for each.

### `GET /api/db/:name`

Returns one record + file size + last modified timestamp.

### `DELETE /api/db/:name`

Sets `status = 'deleted'` in registry. Does not delete file.

### `GET /api/health`

Returns `{ status: 'ok', uptime: number }`.

### `GET /api/metrics`

```json
{
  "total_dbs": 5,
  "active_dbs": 4,
  "volume_used_bytes": 10485760,
  "volume_used_percent": 12.5,
  "largest_db": { "name": "analytics", "size_bytes": 5242880 }
}
```

---

## 9. Disk Usage Helper

```typescript
// src/lib/fs.ts
import fs from 'fs'
import path from 'path'
import { execSync } from 'child_process'

export function getFileSizeBytes(filePath: string): number {
  try {
    return fs.statSync(filePath).size
  } catch {
    return 0
  }
}

export function getVolumeUsagePercent(dataPath: string): number {
  // df -k returns 1k-blocks; parse Use% column
  const out = execSync(`df -k "${dataPath}"`).toString()
  const line = out.split('\n')[1]
  const usePercent = parseInt(line.trim().split(/\s+/)[4])
  return usePercent
}
```

---

## 10. Outerbase Integration

> **TODO:** Confirm exact self-hosted Outerbase API for registering a SQLite data source once their self-hosted docs are reviewed. The integration point is `POST /api/db` — after creating the file, call the Outerbase local API to add it as a connection.

```typescript
// src/lib/outerbase.ts
const OUTERBASE_API = process.env.OUTERBASE_API_URL ?? 'http://localhost:3001'

export async function registerDataSource(name: string, filePath: string) {
  const res = await fetch(`${OUTERBASE_API}/api/v1/connections`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      name,
      type: 'sqlite',
      file: filePath,
    }),
  })
  if (!res.ok) {
    throw new Error(`Outerbase registration failed: ${res.status}`)
  }
  return res.json()
}
```

---

## 11. Environment Variables

| Variable                  | Required | Description                                      |
|---------------------------|----------|--------------------------------------------------|
| `DATA_PATH`               | Yes      | Volume mount path (default: `/data`)             |
| `ADMIN_TOKEN`             | Yes      | Secret used to authenticate via login form       |
| `SESSION_SECRET`          | Yes      | HMAC key for signing session cookies             |
| `MAX_VOLUME_USAGE_PERCENT`| No       | Reject new DBs above this threshold (default 85) |
| `OUTERBASE_API_URL`       | No       | Outerbase internal URL (default: localhost:3001) |
| `PORT`                    | No       | Hono listen port (default: 3000)                 |

---

## 12. Development Setup

```bash
# Install dependencies
npm install

# Copy env
cp .env.example .env

# Run Hono in dev mode (no Outerbase/Caddy needed locally)
npm run dev

# Build
npm run build
```

For local dev, Outerbase and Caddy are optional. Hit Hono directly on port 3000.

---

## 13. Open Questions

- [ ] Confirm Outerbase self-hosted install method (Docker image? npm package?)
- [ ] Confirm Outerbase API endpoint for registering a SQLite data source
- [ ] Decide session cookie `Max-Age` (browser-session vs. persistent)
- [ ] Confirm Railway volume mount path is configurable vs. fixed at `/data`

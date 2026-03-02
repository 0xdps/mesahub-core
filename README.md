# file-db

**A centralized SQLite storage service for internal Railway workloads.**

Each service in your Railway project gets its own SQLite database file, all stored on a single persistent volume and managed through one admin interface.

---

## What it does

- Allocates and tracks SQLite database files under a single `/data` volume
- Exposes a simple REST API so internal services can read from their databases
- Ships a built-in visual browser (forked from [Outerbase Studio](https://github.com/outerbase/studio)) so you can inspect and query any database from the admin dashboard
- Admin dashboard is protected by a single token-based login — no external auth service required

## Stack

| Layer | Technology |
|---|---|
| Framework | Next.js 15 (App Router) |
| SQLite access | `better-sqlite3` (server-side) |
| Session auth | `iron-session` (signed cookies) |
| DB viewer | Outerbase Studio UI (stripped, custom driver) |
| Deploy | Railway + persistent volume |

## Architecture

```
Railway Service
├── /api/db              ← list / create databases
├── /api/db/[name]       ← metadata / soft-delete
├── /api/db/[name]/query ← read-only SQL execution (SELECT only)
├── /api/auth/login      ← token login
├── /api/health          ← health check
├── /api/metrics         ← volume and DB stats
├── / (admin dashboard)  ← DB list, volume usage
├── /db/[name]           ← Studio viewer for any database
└── /login               ← token entry page
```

All `.db` files live at `$DATA_PATH/{name}.db` (default `/data`).  
A `registry.db` meta-database tracks names, owners, and soft-deletes.

## Quick start

```bash
# 1. Clone and install
git clone https://github.com/0xdps/file-db.git
cd file-db
npm install

# 2. Copy env and fill in values
cp .env.example .env.local

# 3. Run dev server
npm run dev
```

Open [http://localhost:3008](http://localhost:3008) — you'll be redirected to `/login`.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `ADMIN_TOKEN` | ✅ | — | Token used to authenticate to the dashboard and API |
| `SESSION_SECRET` | ✅ | — | At least 32-character string for signing session cookies |
| `DATA_PATH` | ❌ | `/data` | Directory where `.db` files are stored |
| `MAX_VOLUME_USAGE_PERCENT` | ❌ | `85` | Block new DB creation above this disk usage % |

## Deployment (Railway)

1. Create a Railway service from this repo
2. Add a **persistent volume** mounted at `/data`
3. Set `ADMIN_TOKEN` and `SESSION_SECRET` in Railway environment variables
4. Deploy — Railway uses `Dockerfile` automatically via `railway.json`

## API reference

### Databases
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/db` | List all active databases |
| `POST` | `/api/db` | Register a new database `{ name, owner?, description? }` |
| `GET` | `/api/db/:name` | Get metadata + file size for a database |
| `DELETE` | `/api/db/:name` | Soft-delete a database |

### Query (read-only)
| Method | Path | Description |
|---|---|---|
| `POST` | `/api/db/:name/query` | Run a SELECT statement `{ sql }` |

### System
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/metrics` | Volume usage + per-DB sizes |

> All API routes except `/api/health` require the `ADMIN_TOKEN` via `Authorization: Bearer <token>` header.

## Project docs

- [PRD.md](./PRD.md) — Product requirements
- [docs/TECH.md](./docs/TECH.md) — Implementation reference

## License

MIT

  - SQLite (local files)
  - Cloudflare D1
  - rqlite
  - StarbaseDB
  - Val.town
- MySQL (beta, limited features)
- PostgreSQL (beta, limited features)

---

Give it a try directly from your browser

[![LibSQL Studio, sqlite online editor](https://github.com/user-attachments/assets/5d92ce58-9ce6-4cd7-9c65-4763d2d3b231)](https://libsqlstudio.com)
[![Libsql studio playground](https://github.com/user-attachments/assets/dcf7e246-fe72-4351-ab10-ae2d1658087d)](https://libsqlstudio.com/playground/client?template=chinook)

## Desktop App

You can download [Windows and Mac desktop app here](https://github.com/outerbase/studio-desktop/releases/).

Outerbase Studio Desktop is a lightweight Electron wrapper for the Outerbase Studio web version. It enables support for drivers that aren't feasible in a browser environment, such as MySQL and PostgreSQL.

## Features

![libsqlstudio-git-preview (7)](https://github.com/user-attachments/assets/1d7a3d90-61e3-4a77-83a5-4bb096bbfb4b)

- **Query Editor**: It features a user-friendly query editor equipped with auto-completion and function hint tooltips. It allows you to execute multiple queries simultaneously and view their results efficiently.
- **Data Editor**: It comes with a powerful data editor, allowing you to stage all your changes and preview them before committing. The data table is highly optimized and lightweight, capable of rendering thousands of rows and columns efficiently.
- **Schema Editor**: It allows you to quickly create, modify, and remove table columns with just a few clicks without writing any SQL.
- **Connection Manager**: It includes a flexible connection manager, allowing you to store your connections locally in your browser. You can also store them on a server and share your connections across multiple devices.

The features mentioned above are just a few of the many we offer. Give it a try to explore everything we have in store

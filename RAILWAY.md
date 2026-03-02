# Deploy and Host sqlite-hub on Railway

sqlite-hub is a self-hosted SQLite management service with a built-in visual studio. Each database is a single `.db` file stored on a Railway volume. Services in your project connect via HTTP using a shared admin token — no separate database infrastructure needed.

## About Hosting sqlite-hub

Hosting sqlite-hub on Railway requires a single service with a persistent volume mounted at `/data`. All SQLite databases are stored as `.db` files on that volume. The app exposes an admin dashboard for browsing, querying, and managing databases, protected by a token-based auth layer. A `SESSION_SECRET` is used to sign session cookies. Both secrets are auto-generated on deploy. The service runs a standalone Next.js server and is accessible over a Railway-generated HTTPS URL.

## Common Use Cases

- **Cross-service shared state** — multiple Railway services write and read a single SQLite file over HTTP without running Postgres
- **Lightweight job queues & feature flags** — store ephemeral application state in a durable flat file with zero ops overhead
- **Internal admin tools** — visually browse and query SQLite databases produced by your backend services, directly in the browser

## Dependencies for sqlite-hub Hosting

- A Railway **persistent volume** mounted at `/data` to store `.db` files across deploys
- `ADMIN_TOKEN` environment variable shared with any service that connects to the sqlite-hub API

### Deployment Dependencies

- [sqlite-hub on GitHub](https://github.com/0xdps/sqlite-hub)
- [Railway Volumes documentation](https://docs.railway.com/volumes)
- [Outerbase Studio (upstream)](https://github.com/outerbase/studio) — the open-source SQL viewer this project is built on

### Implementation Details

Services connect to sqlite-hub by posting SQL to the query API:

```bash
curl -X POST https://your-sqlitehub.railway.app/api/db/mydb/query \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM jobs LIMIT 10"}'
```

Create a new database:

```bash
curl -X POST https://your-sqlitehub.railway.app/api/db \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "mydb", "owner": "billing-service"}'
```

## Why Deploy sqlite-hub on Railway?

Railway is a singular platform to deploy your infrastructure stack. Railway will host your infrastructure so you don't have to deal with configuration, while allowing you to vertically and horizontally scale it.

By deploying sqlite-hub on Railway, you are one step closer to supporting a complete full-stack application with minimal burden. Host your servers, databases, AI agents, and more on Railway.

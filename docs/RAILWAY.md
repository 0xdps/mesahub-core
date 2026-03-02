# Deploy and Host SQLite Hub on Railway

SQLite Hub is a self-hosted SQLite management service with a built-in admin studio. Each database is a single `.db` file served over HTTP. Other services connect using a per-database service secret — no Postgres or external database needed.

## About Hosting SQLite Hub

Hosting SQLite Hub on Railway requires a single service backed by a persistent volume mounted at `/data`. All `.db` files live on that volume and survive deploys. The service exposes an admin dashboard protected by an `ADMIN_TOKEN` (login only) and a `SESSION_SECRET` for encrypted cookies. Each database gets its own `service_secret` that external services use as a Bearer token. The app runs as a standalone Next.js server behind a Railway-generated HTTPS URL.

## Common Use Cases

- **Cross-service shared state** — multiple Railway services read and write a single SQLite file over HTTP without running Postgres
- **Lightweight job queues & feature flags** — durable flat-file storage with zero ops overhead
- **Internal admin tools** — visually browse and query SQLite databases from any service, directly in the browser

## Dependencies for SQLite Hub Hosting

- A Railway **persistent volume** mounted at `/data` to store `.db` files across deploys
- Three environment variables: `DATA_PATH=/data`, `ADMIN_TOKEN=${{ secret(16) }}`, `SESSION_SECRET=${{ secret(32) }}`

### Deployment Dependencies

- [SQLite Hub on GitHub](https://github.com/0xdps/sqlite-hub)
- [Railway Volumes documentation](https://docs.railway.com/volumes)
- [sqlite-hub-client on npm](https://www.npmjs.com/package/sqlite-hub-client) — official Node.js client

### Implementation Details

Install the client in any service that needs to connect:

```bash
npm install sqlite-hub-client
```

```ts
import { connect } from "sqlite-hub-client";

const db = connect({
  url: process.env.SQLITE_HUB_URL,
  token: process.env.SQLITE_HUB_SERVICE_SECRET,
  database: "mydb",
});

await db.insert("events", { name: "signup", created_at: new Date().toISOString() });
```

Set these variables on the consuming service:

```
SQLITE_HUB_URL=https://<your-service>.up.railway.app
SQLITE_HUB_SERVICE_SECRET=shs_<generated in admin dashboard>
```

## Why Deploy SQLite Hub on Railway?

Railway is a singular platform to deploy your infrastructure stack. Railway will host your infrastructure so you don't have to deal with configuration, while allowing you to vertically and horizontally scale it.

By deploying SQLite Hub on Railway, you are one step closer to supporting a complete full-stack application with minimal burden. Host your servers, databases, AI agents, and more on Railway.

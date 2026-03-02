# Deploying sqlite-hub on Railway

sqlite-hub is a self-hosted SQLite service with a built-in admin studio. Each database is a `.db` file on a Railway volume. Services connect over HTTP using a per-database `service_secret` — no separate database infrastructure needed.

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `ADMIN_TOKEN` | ✅ | — | Password to log in to the admin dashboard at `/login`. Does **not** grant API access. |
| `SESSION_SECRET` | ✅ | — | Signing secret for encrypted session cookies. Minimum **32 characters**. Generate with `openssl rand -hex 32`. |
| `DATA_PATH` | ✅ | `/tmp/sqlite-hub-data` | Absolute path where `.db` files are stored. **Must match your Railway volume mount path**, e.g. `/data`. |
| `MAX_VOLUME_USAGE_PERCENT` | ❌ | `85` | Blocks new database creation once the volume exceeds this % of used space. |
| `PORT` | ❌ | `3000` | HTTP port. Railway injects this automatically — do not set it manually. |

> `ADMIN_TOKEN` is **login-only**. Each database gets its own `service_secret` (managed under DB → Settings in the dashboard). That secret is what external services pass as a Bearer token.

---

## Step-by-step Deployment

### 1. Fork / connect the repository

1. Push the sqlite-hub source to your own GitHub repository.
2. In Railway, click **New Project → Deploy from GitHub repo** and select your repository.
3. Railway detects the `Dockerfile` automatically via `railway.toml` — no extra config needed.

---

### 2. Add a persistent volume

SQLite files must survive deploys and restarts. Without a volume every redeploy wipes everything.

1. Open your sqlite-hub service in Railway.
2. Go to **Settings → Volumes → Add Volume**.
3. Set **Mount Path** to `/data`.
4. Click **Add**.

This provisions a persistent volume mounted at `/data`.

---

### 3. Set environment variables

In your Railway service go to **Variables** and add:

```
DATA_PATH=/data
ADMIN_TOKEN=${{ secret(16) }}
SESSION_SECRET=${{ secret(32) }}
```

Optionally:

```
MAX_VOLUME_USAGE_PERCENT=85
```

> **`${{ secret(n) }}`** is a Railway template expression — paste it as the variable *value* in the Railway UI and Railway will auto-generate a cryptographically random **alphanumeric** string of that length. You can then copy the resolved value from the Variables panel if you need it (e.g. to save your `ADMIN_TOKEN` somewhere safe before using it to log in).

If you prefer to generate secrets yourself:

```bash
openssl rand -hex 32
```

---

### 4. Deploy

Trigger a deploy from the Railway dashboard or by pushing to your connected branch. Railway will:

1. Build the Docker image using the multi-stage `Dockerfile`.
2. Start the server with `node server.js`.
3. Poll the health check at `/api/health` — the deploy is marked successful once it returns `200`.

---

### 5. Open the admin dashboard

Click the Railway-generated public URL (or your custom domain). Go to `/login` and sign in with your `ADMIN_TOKEN`.

From the dashboard you can:

- **Create** a new database (give it a short slug, e.g. `analytics`).
- **Browse** its tables in the built-in Studio.
- **Generate a service secret** under DB → Settings → Service secret.
- **Toggle a database inactive** to block writes while keeping data readable.

---

### 6. Connect a service

Install the client in any other service that needs to read/write a database:

```bash
npm install sqlite-hub-client
```

```ts
import { connect } from "sqlite-hub-client";

const db = connect({
  url: process.env.SQLITE_HUB_URL,
  token: process.env.SQLITE_HUB_SERVICE_SECRET, // per-DB secret from Settings
  database: "analytics",
});

// Safe to call on every startup — uses CREATE TABLE IF NOT EXISTS
await db.createTable("events", [
  { name: "id",         type: "INTEGER", primaryKey: true, autoIncrement: true },
  { name: "name",       type: "TEXT",    notNull: true },
  { name: "created_at", type: "TEXT",    notNull: true },
]);

await db.insert("events", { name: "signup", created_at: new Date().toISOString() });
```

Add these variables to the consuming service in Railway:

```
SQLITE_HUB_URL=https://<your-service>.up.railway.app
SQLITE_HUB_SERVICE_SECRET=shs_<generated-in-admin-ui>
```

---

### 7. Private networking (recommended)

Keep all traffic inside the Railway private network — no public internet exposure:

1. **Disable the public URL** on the sqlite-hub service (Settings → Networking → Remove Domain).
2. Use the internal Railway hostname for `SQLITE_HUB_URL` in your consuming services:
   ```
   SQLITE_HUB_URL=http://sqlite-hub.railway.internal:3000
   ```
3. Only services inside the same Railway project can reach it.

> The admin dashboard becomes unreachable from the browser when the public URL is removed. Re-enable the domain temporarily when you need to use the UI, then remove it again.

---

## Upgrade / Redeploy

The volume is never touched during redeploying. All `.db` files persist across every deploy and restart as long as `DATA_PATH` points to the volume mount.

```bash
# Trigger a redeploy via Railway CLI
railway up
```

---

## Common Use Cases

- **Cross-service shared state** — multiple services read/write a single SQLite file over HTTP without running Postgres.
- **Lightweight job queues & feature flags** — durable flat-file storage with zero ops overhead.
- **Internal admin tools** — visually browse and query SQLite databases directly in the browser.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Health check fails on deploy | `DATA_PATH` directory doesn't exist | Confirm volume is mounted at `/data` and `DATA_PATH=/data` is set |
| `401 Unauthorized` from client | Wrong or missing `service_secret` | Regenerate secret under DB → Settings, update `SQLITE_HUB_SERVICE_SECRET` |
| `503 Service Unavailable` | Database is set to **inactive** | Re-activate under DB → Settings → Status |
| Dashboard unreachable | Public URL removed | Temporarily re-enable domain in Railway → Networking |
| Data lost after redeploy | Volume not attached | Add a Railway Volume with mount path `/data` |
| Database creation blocked | Volume above `MAX_VOLUME_USAGE_PERCENT` | Increase volume size in Railway or raise the threshold |

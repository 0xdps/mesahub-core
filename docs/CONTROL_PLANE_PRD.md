# sqlite-hub Control Plane — Product Requirement Document

**Date:** April 2026  
**Status:** Planning  
**Owner:** SaaS Team

---

## 1. Executive Summary

**sqlite-hub Control Plane** is a SaaS service that transforms sqlite-hub from a self-hosted template into a **cloud-hosted, multi-tenant platform**. Users sign up, purchase plans, create databases, and access them via a web dashboard, REST API, or client SDK.

### Key Differentiator
- **sqlite-hub template** remains unchanged (deployable, self-hosted)
- **Control plane** is a separate SaaS service (Vercel) that manages users, billing, and database provisioning
- **Unified auth** via NubeAuth (OAuth + payments included)

---

## 2. Problem Statement

### Current State
- sqlite-hub is a self-hosted template (Railway)
- No multi-user support
- No billing/subscriptions
- Requires manual database setup
- No programmatic access (API keys)

### Target Problem
Users want:
1. **Signup once** → get a managed SQLite account
2. **Create databases** on-demand (no DevOps required)
3. **Scale dynamically** based on usage
4. **Pay as you grow** (free tier available)
5. **Access DBs programmatically** (API + SDK)
6. **View/query via UI** (browser dashboard)

---

## 3. Solution Overview

Two-service architecture:

```
┌──────────────────────────────────┐
│  Vercel                          │
│  sqlite-hub Control Plane        │
│  ├─ NubeAuth OAuth               │
│  ├─ API gateway                  │
│  ├─ Database provisioning        │
│  └─ User management              │
└──────────────────────────────────┘
         ↓ HTTP (Bearer token)
         ↓
┌──────────────────────────────────┐
│  Railway                         │
│  sqlite-hub instances            │
│  ├─ /data/registry.db            │
│  ├─ /data/_control.db (SaaS m/d) │
│  └─ /data/user-123/db.db         │
└──────────────────────────────────┘
```

---

## 4. Pricing Plans

| Feature | Developer (Free) | Pro ($9/mo) | Enterprise (Custom) |
|---------|---|---|---|
| **Databases** | 3 | 20 | Unlimited |
| **Storage** | 1 GB | 50 GB | Unlimited |
| **API Calls/mo** | 10,000 | 1,000,000 | Unlimited |
| **File Upload** | 100 MB/file | 500 MB/file | 5 GB/file |
| **Concurrent Queries** | 5 | 50 | Unlimited |
| **Support** | Community | Email | 24/7 Priority |
| **Seat Licenses** | 1 | 5 | Unlimited |

**Future pricing tiers:**
- Usage-based overages
- Annual billing (20% discount)
- Team plans

---

## 5. Architecture

### 5.1 Services

#### A. **sqlite-hub-control-plane** (Vercel)
- Node.js / Next.js 15
- React 19 frontend (dashboard)
- NubeAuth SDK integration
- API gateway layer
- No local database (uses sqlite-hub for persistence)

#### B. **sqlite-hub** (Railway - template)
- Existing codebase unchanged
- Hosts `/data` volume with all DBs
- Exposes read-only query API
- Manages `_control.db` via HTTP (bearer token auth)
- Can scale horizontally (multiple instances)

### 5.2 Data Storage

All metadata lives in **`/data/_control.db`** on sqlite-hub:

```sql
-- Users (synced from NubeAuth)
CREATE TABLE users (
  id TEXT PRIMARY KEY,              -- from NubeAuth
  email TEXT UNIQUE NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  nube_plan TEXT,                   -- developer | pro | enterprise
  nube_status TEXT                  -- active | trialing | past_due
);

-- API Keys for programmatic access
CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  key_hash TEXT UNIQUE NOT NULL,    -- bcrypt hash
  name TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  last_used_at DATETIME,
  status TEXT DEFAULT 'active'      -- active | revoked
);

-- Database ownership & provisioning metadata
CREATE TABLE databases (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,               -- URL-safe name
  display_name TEXT,                -- user-facing name
  description TEXT,
  sqlite_hub_instance_id TEXT,      -- which instance hosts this
  service_secret TEXT UNIQUE NOT NULL, -- bearer token for sqlite-hub API
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME,
  status TEXT DEFAULT 'active',     -- active | deleted | suspended
  size_bytes INTEGER DEFAULT 0,     -- last known size
  UNIQUE(user_id, name)
);

-- sqlite-hub instance registry
CREATE TABLE instances (
  id TEXT PRIMARY KEY,
  url TEXT UNIQUE NOT NULL,         -- https://hub-1.railway.app
  region TEXT,                      -- us-east, eu-west, ap-south
  admin_token TEXT,                 -- ADMIN_TOKEN from sqlite-hub .env
  max_databases INTEGER,
  current_databases_count INTEGER DEFAULT 0,
  is_available BOOLEAN DEFAULT true,
  health_check_at DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Monthly usage tracking (for billing)
CREATE TABLE usage (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  period_year INT,
  period_month INT,
  queries_executed INTEGER DEFAULT 0,
  api_calls INTEGER DEFAULT 0,
  storage_bytes INTEGER DEFAULT 0,
  UNIQUE(user_id, period_year, period_month)
);

-- Session/auth tokens (if needed for temporary access)
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  token TEXT UNIQUE NOT NULL,
  expires_at DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 6. Authentication & Authorization

### 6.1 User Signup/Login Flow
1. User visits `https://sqlite-hub.io`
2. Clicks "Sign Up"
3. Redirected to NubeAuth OAuth
4. User authenticates (Google/GitHub/etc)
5. NubeAuth redirects back with session cookie
6. Control-plane creates user record in `_control.db`
7. User lands on dashboard

### 6.2 API Key Flow (for CLI/SDKs)
1. User generates API key in dashboard settings
2. Key stored as bcrypt hash in `_control.db`
3. SDK/CLI uses: `Authorization: Bearer sk_live_abc123...`
4. Control-plane validates key → routes to sqlite-hub

### 6.3 Authorization Rules
```typescript
const rules = {
  // User can only access their own databases
  can_view_db: (user.id === db.user_id),
  can_query_db: (user.id === db.user_id) && (user.plan.api_calls_remaining > 0),
  can_create_db: (user.databases.length < plan.max_databases),
  can_upload_file: (user.storage_used_bytes + file_size <= plan.max_storage),
};
```

---

## 7. API Specification

### 7.1 Authentication Endpoints

```
POST /api/auth/nube-login
  → Redirect to NubeAuth OAuth URL

POST /api/auth/nube-callback
  → NubeAuth redirects here with code
  → Exchange code for session
  → Create user in _control.db if new

POST /api/auth/logout
  → Clear session

GET /api/auth/me
  → Return { id, email, plan, subscription_status }
```

### 7.2 User Endpoints

```
GET /api/user/profile
  → { id, email, plan, created_at }

GET /api/user/subscription
  → { plan, status, period_end, usage }

POST /api/user/api-keys
  → Create new API key
  → Returns plain key (only time!) + hash stored

GET /api/user/api-keys
  → List all keys for user (hash only, no plain text)

DELETE /api/user/api-keys/:keyId
  → Revoke key
```

### 7.3 Database Endpoints

```
POST /api/user/databases
  Body: { name, display_name?, description? }
  → Allocates DB on least-loaded sqlite-hub instance
  → Creates user directory in /data
  → Returns { id, name, serviceUrl, api_docs_url }

GET /api/user/databases
  → List all user's databases
  → Returns: [ { id, name, created_at, size_bytes, status } ]

GET /api/user/databases/:dbId
  → Get single DB metadata

DELETE /api/user/databases/:dbId
  → Soft-delete (mark as deleted, keep files)

GET /api/user/databases/:dbId/stats
  → { size_bytes, query_count, api_calls_month }
```

### 7.4 Query Proxy Endpoints

```
POST /api/user/databases/:dbId/query
  Header: Authorization: Bearer <api_key>
  Body: { sql, params? }
  → Validates user quota
  → Routes to sqlite-hub instance
  → Tracks usage
  → Returns { rows, columns, execution_time_ms }

POST /api/user/databases/:dbId/files
  → Upload file (multipart form data)
  → Returns file ID + metadata

GET /api/user/databases/:dbId/files
  → List files in database

GET /api/user/databases/:dbId/files/:fileId
  → Download file

DELETE /api/user/databases/:dbId/files/:fileId
  → Delete file
```

### 7.5 Billing Endpoints

```
GET /api/billing/plans
  → List all available plans

POST /api/billing/checkout
  → Initiate upgrade flow
  → Redirects to NubeAuth checkout (Stripe/Dodo/LemonSqueezy)

GET /api/billing/subscription
  → Current subscription + renewal date

POST /api/billing/cancel
  → Cancel subscription
```

---

## 8. Frontend (Dashboard)

### 8.1 Pages

| Route | Purpose | Auth |
|-------|---------|------|
| `/` | Landing page | Public |
| `/login` | OAuth login | Public |
| `/dashboard` | User home page | Required |
| `/databases` | DB management (create, list, delete) | Required |
| `/db/[id]` | DB viewer (query editor + results) | Required |
| `/settings/api-keys` | API key management | Required |
| `/settings/subscription` | Plan info, upgrade button | Required |
| `/pricing` | Pricing page + upgrade | Public |
| `/docs` | API documentation | Public |

### 8.2 Components

**Dashboard:**
- Quick stats (DBs count, storage used, API calls remaining)
- Recent activities
- Upgrade CTA (if free plan)

**Database List:**
- Table of user's databases
- Columns: name, created, size, status
- Actions: view, delete, rename, download

**DB Viewer:**
- Reuse Outerbase UI component (from @sqlite-hub/ui)
- Table browser (left sidebar)
- SQL editor (top)
- Results grid (bottom)
- Query history

**API Keys:**
- List API keys with last-used date
- Generate new key (copy to clipboard)
- Revoke key with confirmation

**Subscription:**
- Current plan badge
- Usage meters (DBs, storage, API calls)
- Upgrade / downgrade buttons
- Billing history

---

## 9. Implementation Phases

### Phase 1: Project Setup (2 days)
- [x] Create control-plane repo
- [ ] Set up Next.js with Tailwind CSS
- [ ] Configure NubeAuth SDK
- [ ] Set up `_control.db` schema on sqlite-hub
- [ ] Deploy skeleton to Vercel

### Phase 2: Authentication (3 days)
- [ ] NubeAuth OAuth flow (web)
- [ ] Session management (iron-session)
- [ ] User sync from NubeAuth → `_control.db`
- [ ] `/api/auth/login`, `/api/auth/callback`, `/api/auth/logout`
- [ ] Middleware for protected routes
- [ ] Tests for auth flow

### Phase 3: Database Provisioning (4 days)
- [ ] Instance registry (load balancing logic)
- [ ] DB creation API (`POST /api/user/databases`)
- [ ] Service secret generation + storage
- [ ] `/data/user-123/` directory creation on sqlite-hub
- [ ] Soft-delete logic
- [ ] Tests for provisioning

### Phase 4: API Key Management (2 days)
- [ ] API key generation + bcrypt hashing
- [ ] Bearer token validation middleware
- [ ] `/api/user/api-keys` CRUD endpoints
- [ ] Key revocation
- [ ] Tests

### Phase 5: Query Proxy (3 days)
- [ ] `queryControlDb()` helper function
- [ ] `POST /api/user/databases/:dbId/query` endpoint
- [ ] Bearer token routing to sqlite-hub
- [ ] Usage tracking (queries, API calls)
- [ ] Quota enforcement
- [ ] Tests

### Phase 6: File Management (2 days)
- [ ] `/api/user/databases/:dbId/files` endpoints
- [ ] File upload to sqlite-hub
- [ ] File download proxy
- [ ] Storage quota checks

### Phase 7: Dashboard UI (5 days)
- [ ] Layout + navigation
- [ ] Database list page
- [ ] DB viewer page (integrate Outerbase UI)
- [ ] API keys page
- [ ] Subscription page
- [ ] Settings page

### Phase 8: Billing Integration (2 days)
- [ ] NubeAuth checkout flow
- [ ] Plan limits enforcement in API
- [ ] Usage tracking + reset monthly
- [ ] Subscription status check

### Phase 9: Client Package (3 days)
- [ ] `@sqlite-hub/client` package
- [ ] TypeScript SDK for Node.js + browser
- [ ] Query builder helper
- [ ] Error handling + retry logic
- [ ] Examples + tests

### Phase 10: Docs + Polish (3 days)
- [ ] API documentation (OpenAPI / Swagger)
- [ ] SDK examples (Node.js, Python, cURL)
- [ ] Deployment guide
- [ ] Database schema docs
- [ ] Troubleshooting guide

**Total Estimate:** ~29 days (4-5 weeks)

---

## 10. Security & Compliance

### 10.1 Key Security Measures
- ✅ API keys hashed with bcrypt (never stored plain)
- ✅ Bearer token validation on every API call
- ✅ User isolation (queries filtered by user_id)
- ✅ CORS configured appropriately
- ✅ Rate limiting per API key
- ✅ HTTPS enforced
- ✅ Input validation + SQL injection prevention

### 10.2 Multi-tenancy
- All queries scoped to `user_id`
- Database names isolated per user (`/data/user-123/db.db`)
- Service secrets unique per database
- No cross-user data sharing

### 10.3 Logging & Monitoring
- Query execution logged (for audit)
- API call metrics tracked
- Error tracking (Sentry)
- Performance monitoring (Vercel Analytics)

---

## 11. Deployment

### 11.1 Control-Plane (Vercel)

**Environment Variables:**
```
NUBE_GATEWAY_URL=https://api.nubeauth.com
NUBE_APP_ID=app_xxx
SQLITE_HUB_URL=https://sqlite-hub.railway.app
CONTROL_DB_SECRET=sk_control_xxx
CONTROL_DB_NAME=_control
ADMIN_TOKEN_CONTROL_PLANE=... (for management APIs)
```

**Deployment:**
```bash
git push origin main
# Vercel auto-deploys
# DNS: sqlite-hub.io → Vercel
```

### 11.2 sqlite-hub (Railway template - unchanged)

Add to `.env`:
```
_CONTROL_DB_SERVICE_SECRET=sk_control_xxx
```

Create `_control.db` on first startup:
```bash
# In start.sh, before app starts:
sqlite3 /data/_control.db < /app/schema.sql
```

---

## 12. Success Metrics

| Metric | Target |
|--------|--------|
| Signups (first month) | 50 users |
| Free → Pro conversion | 15% |
| API uptime | 99.5% |
| Query latency (p95) | < 500ms |
| User retention (30d) | > 70% |
| Support response time | < 24h |

---

## 13. Future Roadmap (Phase 2+)

- [ ] Teams / workspace support (multi-user accounts)
- [ ] Database sharing (read-only links)
- [ ] Data backup / point-in-time restore
- [ ] Custom domain support
- [ ] Usage-based billing (pay-per-query)
- [ ] Social login (Google, GitHub, Magic Link)
- [ ] Slack / Discord integrations
- [ ] GraphQL API
- [ ] WebSocket subscriptions for real-time queries
- [ ] Open-source community version

---

## 14. Risks & Mitigation

| Risk | Severity | Mitigation |
|------|----------|-----------|
| sqlite-hub instance overload | High | Load balancing + auto-scaling |
| Data loss (no backup) | Critical | Railway volume snapshots + export APIs |
| Performance degradation | High | Query caching + indexing |
| Account takeover | High | 2FA via NubeAuth |
| Cross-tenant data leak | Critical | Strict user_id filtering + tests |
| Billing errors | Medium | NubeAuth handles; we validate |

---

## 15. Glossary

- **sqlite-hub**: Self-hosted Railway template (storage layer)
- **Control Plane**: SaaS service on Vercel (management layer)
- **Service Secret**: Bearer token used by control-plane to access sqlite-hub DBs
- **API Key**: Bearer token users get to access their DBs programmatically
- **Instance**: A sqlite-hub deployment on Railway
- **Shard**: Logical grouping of instances for load balancing
- `_control.db`: Metadata database inside sqlite-hub for SaaS management

---

## Appendix A: Example User Journey

```
1. User visits sqlite-hub.io
   ↓
2. Clicks "Sign Up" → NubeAuth OAuth
   ↓
3. Picks "Developer (Free)" plan
   ↓
4. Lands on dashboard
   ↓
5. Clicks "New Database"
   ↓
6. Names it "my-app"
   ↓
7. Control-plane:
   - Allocates on least-loaded instance
   - Creates /data/user-123/my-app.db
   - Generates service secret
   - Records in _control.db
   ↓
8. User sees "Database created!"
   ↓
9. Clicks "View" → DB viewer with SQL editor
   ↓
10. Executes query: SELECT * FROM users
    - Routed through control-plane API
    - Control-plane uses service_secret
    - sqlite-hub executes
    - Results returned
    ↓
11. User generates API key in settings
    ↓
12. Uses in Node.js:
    const client = new SqliteHub({ apiKey: "sk_live_..." });
    const result = await client.databases.get("my-app").query("SELECT ...");
```

---

**End of PRD**

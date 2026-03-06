# sqlite-hub-client

High-level TypeScript/JavaScript client for [sqlite-hub](https://github.com/0xdps/sqlite-hub).  
Comes with a full set of APIs for schema management, reads, writes — all using an **adapter** abstraction so the same code works over HTTP today and can talk to SQLite directly later.

## Install

```bash
npm install sqlite-hub-client
```

## Quick start

```ts
import { connect } from "sqlite-hub-client";

const db = connect({
  url:   process.env.SQLITE_HUB_URL,            // e.g. https://my-app.up.railway.app
  token: process.env.SQLITE_HUB_SERVICE_SECRET, // service_secret from admin Settings
  db:    "my-service",
});

// File upload
const uploaded = await db.files!.upload({
  file: new Blob(["hello world"], { type: "text/plain" }),
  filename: "hello.txt",
  folderPath: "docs/examples",
  metadata: { source: "docs" },
});

// File list
const filePage = await db.files!.list({
  limit: 20,
  offset: 0,
  folderPrefix: "docs",
});

// Presigned browser URL (for third-party dashboards)
const signed = await db.files!.presign(uploaded.id, { expiresIn: 1800 });
console.log(signed.url);

// Create table
await db.createTable("users", [
  { name: "id", type: "INTEGER", primaryKey: true, autoIncrement: true },
  { name: "email", type: "TEXT", notNull: true, unique: true },
  { name: "name", type: "TEXT" },
  { name: "created_at", type: "TEXT", default: "(datetime('now'))" },
]);

// Create index
await db.createIndex("idx_users_email", "users", ["email"], { unique: true });

// Insert
const { lastInsertRowid } = await db.insert("users", {
  email: "alice@example.com",
  name: "Alice",
});

// Bulk insert
await db.insertMany("users", [
  { email: "bob@example.com", name: "Bob" },
  { email: "carol@example.com", name: "Carol" },
]);

// Read — find all
const users = await db.find<{ id: number; email: string; name: string }>(
  "users"
);

// Read — filtered, paginated, ordered
const page = await db.find(
  "users",
  {},
  {
    orderBy: "created_at",
    order: "DESC",
    limit: 10,
    offset: 0,
  }
);

// Read — single row by arbitrary filter
const alice = await db.findOne("users", { email: "alice@example.com" });

// Read — by primary key
const user = await db.findById("users", 1);

// Count
const total = await db.count("users");
const active = await db.count("users", { active: 1 });

// Exists check
const exists = await db.exists("users", { email: "alice@example.com" });

// Upsert — INSERT OR REPLACE (relies on PRIMARY KEY / UNIQUE constraints)
await db.upsert("users", { id: 1, email: "alice@example.com", name: "Alice" });

// Upsert — explicit conflict target
await db.upsert(
  "users",
  { email: "alice@example.com", name: "Alice" },
  { conflictColumns: ["email"] }
);

// Bulk upsert
await db.upsertMany(
  "users",
  [
    { email: "alice@example.com", name: "Alice" },
    { email: "bob@example.com",   name: "Bob"   },
  ],
  { conflictColumns: ["email"] }
);

// Update
await db.update("users", { name: "Alice Smith" }, { id: 1 });

// Delete
await db.delete("users", { id: 1 });

// Drop table
await db.dropTable("users");

// Raw SQL escape hatch
const result = await db.exec("PRAGMA table_info(users)");
```

## API

### `connect(options)` — create a database client

| Option    | Type     | Required | Description                                                                                     |
| --------- | -------- | -------- | ----------------------------------------------------------------------------------------------- |
| `url`     | `string` | ✅       | Base URL of your sqlite-hub deployment                                                          |
| `token`   | `string` | ✅       | Per-DB `service_secret` — generate one in the admin dashboard under Settings → Service secret |
| `db`      | `string` | ✅       | Name of the database to operate on                                                              |
| `timeout` | `number` | ❌       | Request timeout in ms (default: `10000`)                                                        |

---

### Schema

#### `db.createTable(table, columns, options?)`

```ts
await db.createTable(
  "posts",
  [
    { name: "id", type: "INTEGER", primaryKey: true, autoIncrement: true },
    { name: "title", type: "TEXT", notNull: true },
    { name: "body", type: "TEXT" },
  ],
  { ifNotExists: true }
); // ifNotExists: true is the default
```

**`ColumnDef` fields:**

| Field           | Type               | Description                                    |
| --------------- | ------------------ | ---------------------------------------------- |
| `name`          | `string`           | Column name                                    |
| `type`          | `string`           | SQLite type: `INTEGER`, `TEXT`, `REAL`, `BLOB` |
| `primaryKey`    | `boolean`          | Mark as PRIMARY KEY                            |
| `autoIncrement` | `boolean`          | Add AUTOINCREMENT                              |
| `notNull`       | `boolean`          | Add NOT NULL constraint                        |
| `unique`        | `boolean`          | Add UNIQUE constraint                          |
| `default`       | `string \| number` | DEFAULT value (raw SQL fragment)               |

#### `db.dropTable(table, ifExists?)`

```ts
await db.dropTable("posts"); // IF EXISTS by default
```

#### `db.createIndex(indexName, table, columns, options?)`

```ts
await db.createIndex("idx_posts_title", "posts", ["title"]);
await db.createIndex("idx_unique_email", "users", ["email"], { unique: true });
```

#### `db.dropIndex(indexName, ifExists?)`

```ts
await db.dropIndex("idx_posts_title");
```

---

### Write

#### `db.insert(table, data)` → `ExecResult`

```ts
const { lastInsertRowid } = await db.insert("posts", {
  title: "Hello",
  body: "World",
});
```

#### `db.insertMany(table, rows)` → `ExecResult`

```ts
await db.insertMany("posts", [
  { title: "Post 1", body: "..." },
  { title: "Post 2", body: "..." },
]);
```

#### `db.upsert(table, data, options?)` → `ExecResult`

Insert a row or update it on conflict.

```ts
// INSERT OR REPLACE — relies on PRIMARY KEY / UNIQUE constraints
await db.upsert("users", { id: 1, email: "alice@example.com", name: "Alice" });

// INSERT … ON CONFLICT(email) DO UPDATE SET name = excluded.name
await db.upsert(
  "users",
  { email: "alice@example.com", name: "Alice" },
  { conflictColumns: ["email"] }
);
```

#### `db.upsertMany(table, rows, options?)` → `ExecResult`

Bulk version of `upsert`.

```ts
await db.upsertMany(
  "users",
  [
    { email: "alice@example.com", name: "Alice" },
    { email: "bob@example.com",   name: "Bob"   },
  ],
  { conflictColumns: ["email"] }
);
```

**`UpsertOptions` fields:**

| Field              | Type       | Description                                                                                                     |
| ------------------ | ---------- | --------------------------------------------------------------------------------------------------------------- |
| `conflictColumns`  | `string[]` | Conflict-target columns. Omit to use `INSERT OR REPLACE` (relies on PRIMARY KEY / UNIQUE). |
| `updateColumns`    | `string[]` | Columns to overwrite on conflict. Defaults to all columns **not** in `conflictColumns`. Pass `[]` for DO NOTHING. |

---

#### `db.update(table, data, where)` → `ExecResult`

```ts
const { rowsAffected } = await db.update(
  "posts",
  { title: "Updated" },
  { id: 1 }
);
```

#### `db.delete(table, where)` → `ExecResult`

```ts
await db.delete("posts", { id: 1 });
```

---

### Read

#### `db.find<T>(table, where?, options?)` → `T[]`

```ts
const posts = await db.find<Post>(
  "posts",
  { published: 1 },
  {
    columns: ["id", "title"],
    orderBy: "created_at",
    order: "DESC", // "ASC" | "DESC"
    limit: 20,
    offset: 0,
  }
);
```

#### `db.findOne<T>(table, where?)` → `T | null`

```ts
const post = await db.findOne<Post>("posts", { id: 5 });
```

#### `db.findById<T>(table, id, idColumn?)` → `T | null`

```ts
const post = await db.findById<Post>("posts", 5); // uses "id" column
const item = await db.findById<Item>("items", "abc", "slug"); // custom PK column
```

#### `db.count(table, where?)` → `number`

```ts
const total = await db.count("posts");
const drafts = await db.count("posts", { published: 0 });
```

#### `db.exists(table, where)` → `boolean`

```ts
const taken = await db.exists("users", { email: "alice@example.com" });
```

---

### Raw SQL

#### `db.exec<T>(sql, bindings?)` → `QueryResult<T> | ExecResult`

```ts
// SELECT → QueryResult
const result = await db.exec<{ n: number }>("SELECT COUNT(*) AS n FROM posts");

// DDL / DML → ExecResult
await db.exec("CREATE INDEX IF NOT EXISTS idx_title ON posts (title)");
```

---

### Files

#### `db.files.upload(input)`

```ts
const uploaded = await db.files!.upload({
  file: new Blob(["report"], { type: "text/plain" }),
  filename: "report.txt",
  folderPath: "ops/reports",
  conflictMode: "replace", // or "error"
  contentType: "text/plain",
  metadata: { owner: "ops" },
  expiresIn: 3600,
});
```

#### `db.files.list(options?)`

```ts
const files = await db.files!.list({
  limit: 50,
  offset: 0,
  sort: "uploaded_at",
  order: "desc",
  folderPrefix: "ops",
});
```

#### `db.files.getMeta(fileId)`

```ts
const meta = await db.files!.getMeta("file-id");
```

#### `db.files.delete(fileId)`

```ts
await db.files!.delete("file-id");
```

#### `db.files.bulkDelete(fileIds)`

```ts
await db.files!.bulkDelete(["id1", "id2"]);
```

#### `db.files.presign(fileId, options?)`

Returns a signed URL that can be opened directly in browser without bearer token.

```ts
const signed = await db.files!.presign("file-id", {
  expiresIn: 900,
  disposition: "inline", // or "attachment"
});

// e.g. embed in dashboard iframe/image link
console.log(signed.url);
```

#### `db.files.batchPresign(options)`

Generate presigned URLs for multiple files at once. Maximum 100 file IDs per request.

```ts
const batch = await db.files!.batchPresign({
  fileIds: ["id1", "id2", "id3"],
  expiresIn: 900,
  disposition: "inline",
});

// Check results
batch.results.forEach((result) => {
  if (result.error) {
    console.error(`Failed for ${result.file_id}: ${result.error}`);
  } else {
    console.log(`URL for ${result.file_id}: ${result.url}`);
  }
});
```

#### `db.files.createFileAccessToken(options?)`

Creates a long-lived access token for file operations. Unlike presigned URLs which are per-file, this token works for all file operations on the database.

```ts
const tokenData = await db.files!.createFileAccessToken({
  scope: "files:read",
  expiresIn: 2592000, // 30 days
  description: "Dashboard access",
});

// Use in query parameter or Authorization header
const fileUrl = `https://host/api/db/mydb/files/file-id?token=${tokenData.token}`;

// Or with Authorization header:
// Authorization: Bearer <token>
```

The token is read-only (`files:read`) and cannot be used for upload/delete operations. Default TTL is 30 days, maximum is 1 year.

#### `db.files.revokeFileAccessToken(options)`

Revokes a previously issued file access token.

```ts
// Revoke using the original token string
await db.files!.revokeFileAccessToken({
  token: tokenData.token,
  reason: "rotated credential",
});

// Or revoke by token ID + expiresAt if stored separately
await db.files!.revokeFileAccessToken({
  tokenId: "token-id",
  expiresAt: tokenData.expires_at,
  reason: "compromised",
});
```

#### `db.files.getDownloadUrl(fileId)`

Returns the authenticated endpoint URL (requires bearer auth unless using presigned query).

```ts
const url = db.files!.getDownloadUrl("file-id");
```

---

## Architecture

```
sqlite-hub-client
├── index.ts              ← connect() factory + all public exports
├── database.ts           ← Database class — all high-level APIs
└── adapters/
    ├── types.ts          ← IAdapter interface (exec only)
    ├── http.ts           ← HttpAdapter (sqlite-hub over HTTP)
    └── index.ts          ← re-exports
```

Adding a direct SQLite adapter in the future is a one-liner:

```ts
// future
import { Database } from "sqlite-hub-client";
import { DirectAdapter } from "sqlite-hub-client/adapters/direct"; // coming soon

const db = new Database(new DirectAdapter({ path: "./local.db" }));
// same API — createTable, find, insert, update, delete…
```

## License


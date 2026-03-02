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
  url: process.env.SQLITE_DB_HUB_URL, // e.g. https://my-app.up.railway.app
  token: process.env.SQLITE_DB_HUB_TOKEN, // ADMIN_TOKEN set on the service
  db: "my-service", // database name
});

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

| Option    | Type     | Required | Description                                           |
| --------- | -------- | -------- | ----------------------------------------------------- |
| `url`     | `string` | ✅       | Base URL of your sqlite-hub deployment             |
| `token`   | `string` | ✅       | `ADMIN_TOKEN` configured on the sqlite-hub service |
| `db`      | `string` | ✅       | Name of the database to operate on                    |
| `timeout` | `number` | ❌       | Request timeout in ms (default: `10000`)              |

Returns a `Database` instance.

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

MIT

## Install

```bash
npm install sqlite-hub-client
```

Or directly from GitHub (before the npm package is published):

```bash
npm install github:0xdps/sqlite-hub-client
```

## Quick start

```ts
import { createClient } from "sqlite-hub-client";

const db = createClient({
  url: process.env.SQLITE_DB_HUB_URL, // e.g. https://my-app.up.railway.app
  token: process.env.SQLITE_DB_HUB_TOKEN, // ADMIN_TOKEN set on the service
  db: "my-service", // name of the database to use
});

// Create a table
await db.run(`
  CREATE TABLE IF NOT EXISTS users (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    name  TEXT
  )
`);

// Insert a row
await db.run("INSERT INTO users (email, name) VALUES (?, ?)", [
  "alice@example.com",
  "Alice",
]);

// Query rows (typed)
const users = await db.query<{ id: number; email: string; name: string }>(
  "SELECT * FROM users"
);

// Query a single row (or null)
const user = await db.queryOne<{ id: number; email: string }>(
  "SELECT * FROM users WHERE email = ?",
  ["alice@example.com"]
);
```

## API

### `createClient(options)` / `new FileDbClient(options)`

| Option    | Type     | Required | Description                                           |
| --------- | -------- | -------- | ----------------------------------------------------- |
| `url`     | `string` | ✅       | Base URL of your sqlite-hub deployment             |
| `token`   | `string` | ✅       | `ADMIN_TOKEN` configured on the sqlite-hub service |
| `db`      | `string` | ✅       | Name of the database to operate on                    |
| `timeout` | `number` | ❌       | Request timeout in ms (default: `10000`)              |

---

### `db.exec(sql, bindings?)`

Run any SQL statement. Returns a `QueryResult` for SELECT, or an `ExecResult` for writes.

```ts
const result = await db.exec("SELECT count(*) as n FROM users");
// { headers: [...], rows: [{ n: 1 }], rowsRead: 1 }
```

---

### `db.query<T>(sql, bindings?)`

Run a SELECT and return typed rows.

```ts
const rows = await db.query<{ id: number; name: string }>(
  "SELECT id, name FROM users WHERE id > ?",
  [5]
);
```

---

### `db.queryOne<T>(sql, bindings?)`

Run a SELECT and return the first row, or `null` if no results.

```ts
const row = await db.queryOne<{ name: string }>(
  "SELECT name FROM users WHERE id = ?",
  [1]
);
```

---

### `db.run(sql, bindings?)`

Run a write statement (INSERT, UPDATE, DELETE, CREATE, ALTER, DROP). Returns `{ rowsAffected, lastInsertRowid }`.

```ts
const { rowsAffected, lastInsertRowid } = await db.run(
  "INSERT INTO jobs (payload) VALUES (?)",
  [JSON.stringify({ task: "send-email" })]
);
```

## Types

```ts
interface QueryResult<T> {
  headers: ColumnHeader[];
  rows: T[];
  rowsRead: number;
}

interface ExecResult {
  rowsAffected: number;
  lastInsertRowid: number | null;
}

interface ColumnHeader {
  name: string;
  displayName: string;
  originalType: string | null;
}
```

## License

MIT

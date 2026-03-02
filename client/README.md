# sqlite-db-hub-client

Lightweight TypeScript/JavaScript client SDK for [sqlite-db-hub](https://github.com/0xdps/sqlite-db-hub) — SQLite over HTTP.

## Install

```bash
npm install sqlite-db-hub-client
```

Or directly from GitHub (before the npm package is published):

```bash
npm install github:0xdps/sqlite-db-hub-client
```

## Quick start

```ts
import { createClient } from "sqlite-db-hub-client";

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
| `url`     | `string` | ✅       | Base URL of your sqlite-db-hub deployment             |
| `token`   | `string` | ✅       | `ADMIN_TOKEN` configured on the sqlite-db-hub service |
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

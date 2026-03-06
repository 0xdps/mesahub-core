# FILE_STORAGE.md

This document explains the file upload/download capability added to sqlite-hub, including API behavior, storage layout, limits, and SDK guidance.

## 1. What was added

sqlite-hub now supports per-database file storage with:

- Upload files into a DB-scoped namespace
- List files with pagination/sorting
- Download files by ID
- Retrieve file metadata
- Delete one or many files
- File metrics in the global metrics endpoint

Storage is content-addressed (SHA-256 hash) with deduplication, so duplicate file bytes are stored once and reference-counted.

## 2. Storage model

- Data root: `DATA_PATH/files`
- Blob files: `DATA_PATH/files/blobs/<sha256>`
- Metadata DB: `DATA_PATH/files/metadata.db`

Metadata schema (internal):

- `files`: file row per upload (id, db_name, filename, metadata, expiry, hash, size)
- `blob_refs`: one row per blob hash with `ref_count`

Lifecycle behavior:

- Upload increments blob ref count (or creates first ref)
- Delete decrements ref count and removes physical blob when it reaches `0`
- DB soft-delete and hard-delete both trigger file cleanup for that DB

## 3. Authentication and access

File endpoints use the same DB authorization as query/exec endpoints:

- Admin session: allowed
- DB `service_secret` bearer token: allowed
- No `service_secret`: internal network only

Inactive DBs reject writes.

## 4. API reference

Base path:

- `/api/db/:name/files`

### 4.1 Upload

- `POST /api/db/:name/files`
- Request: `multipart/form-data`

Form fields:

- `file` (required): binary file
- `filename` (optional): override filename
- `folder_path` (optional): logical folder path like `reports/2026/march`
- `conflict_mode` (optional): `replace | error` (default comes from server config)
- `content_type` (optional)
- `metadata` (optional): JSON object string
- `expires_in` (optional): positive integer seconds

Response (`201`):

```json
{
  "id": "uuid",
  "filename": "report.pdf",
  "folder_path": "reports/2026/march",
  "size_bytes": 12345,
  "content_type": "application/pdf",
  "url": "/api/db/mydb/files/<id>",
  "uploaded_at": "2026-03-06T00:00:00.000Z",
  "expires_at": null
}
```

### 4.2 List files

- `GET /api/db/:name/files?limit=100&offset=0&sort=uploaded_at&order=desc`

Query params:

- `limit` (1..1000)
- `offset` (>= 0)
- `sort`: `uploaded_at | size_bytes | filename | folder_path`
- `order`: `asc | desc`
- `folder_prefix` (optional): includes files in that folder and subfolders

### 4.3 Download (or stream via proxy)

- `GET /api/db/:name/files/:id`
- `HEAD /api/db/:name/files/:id`

Response headers include:

- `Content-Type`
- `Content-Length`
- `Content-Disposition`
- `ETag` (content hash)
- `X-Content-Hash`

When `ENABLE_FILE_PROXY_DELIVERY=true`, the API responds with `X-Sendfile` so the proxy can deliver file bytes efficiently.

### 4.4 File metadata

- `GET /api/db/:name/files/:id/meta`

Returns metadata, folder path, content hash, and expiry fields.

### 4.5 Create presigned URL

- `POST /api/db/:name/files/:id/presign`
- Body (optional):

```json
{
  "expires_in": 900,
  "disposition": "inline"
}
```

Response:

```json
{
  "url": "https://host/api/db/mydb/files/<id>?exp=...&sig=...",
  "expires_at": "2026-03-06T00:00:00.000Z",
  "expires_in": 900,
  "disposition": "inline",
  "token_type": "signed_query"
}
```

Use this URL directly in a browser or third-party dashboard without an `Authorization` header.

### 4.6 Batch presign URLs

- `POST /api/db/:name/files/presign/batch`
- Body:

```json
{
  "file_ids": ["id1", "id2", "id3"],
  "expires_in": 900,
  "disposition": "inline"
}
```

Response:

```json
{
  "results": [
    {
      "file_id": "id1",
      "url": "https://host/api/db/mydb/files/id1?exp=...&sig=...",
      "expires_at": "2026-03-06T00:00:00.000Z",
      "expires_in": 900,
      "disposition": "inline"
    },
    {
      "file_id": "id2",
      "error": "File not found"
    }
  ],
  "token_type": "signed_query",
  "total": 2,
  "successful": 1,
  "failed": 1
}
```

Maximum 100 file IDs per request.

### 4.7 Create file access token

- `POST /api/db/:name/tokens/files`
- Body (optional):

```json
{
  "scope": "files:read",
  "expires_in": 2592000,
  "description": "Dashboard access token"
}
```

Scopes:
- `files:read`: Can list, download, and get metadata for files

File access tokens are read-only. Mutating operations (upload/delete) require standard DB authentication.

Response:

```json
{
  "token": "eyJ...",
  "token_type": "bearer",
  "expires_at": "2026-04-05T00:00:00.000Z",
  "expires_in": 2592000,
  "scope": "files:read",
  "description": "Dashboard access token",
  "usage": "Use as query parameter: ?token=... or Authorization header: \"Bearer <token>\""
}
```

This token can be used for long-term file access in third-party dashboards. Unlike presigned URLs which are per-file, a file access token works for all file operations on the database.

Use the token by either:
- Query parameter: `GET /api/db/mydb/files/:id?token=...`
- Authorization header: `Authorization: Bearer <token>`

The token is stateless and cannot be revoked before expiry. Default TTL is 30 days, maximum is 1 year.

### 4.8 Delete file

- `DELETE /api/db/:name/files/:id`

Returns `204` on success.

### 4.7 Bulk delete
### 4.9 Bulk delete

- `POST /api/db/:name/files/bulk-delete`
- Body:

```json
{
  "file_ids": ["id1", "id2", "id3"]
}
```

Response:

```json
{
  "deleted": 2,
  "failed": 1
}
```

## 5. Limits and validation

Configured via environment variables:

- `FILE_MAX_SIZE_BYTES` (default `104857600`)
- `FILE_MAX_FILES_PER_DB` (default `10000`)
- `FILE_MAX_STORAGE_PER_DB_BYTES` (default `5368709120`)
- `FILE_UPLOAD_CONFLICT_MODE` (default `replace`): behavior when uploading to an existing `db_name + folder_path + filename`
- `FILE_MAX_FILENAME_LENGTH` (default `255`)
- `FILE_MAX_METADATA_BYTES` (default `4096`)
- `FILE_ALLOWED_MIME_PATTERNS` (default allows common image/video/audio/text/pdf/json/zip/gzip)
- `FILE_URL_SIGNING_SECRET` (optional; falls back to `SESSION_SECRET`) - used for presigned URLs
- `FILE_TOKEN_SIGNING_SECRET` (optional; falls back to `FILE_URL_SIGNING_SECRET` or `SESSION_SECRET`) - used for access tokens
- `FILE_PRESIGN_DEFAULT_TTL_SECONDS` (default `900`)
- `FILE_PRESIGN_MAX_TTL_SECONDS` (default `86400`)

Validation includes:

- Non-empty file
- MIME allow-list check
- Per-DB count and byte quota checks
- Metadata size cap
- Conflict handling for same path:
  - `replace`: updates existing logical file record in-place
  - `error`: returns `409 file already exists in this folder`

## 6. Metrics

`GET /api/metrics` now includes:

- `files.total_files`
- `files.total_bytes`
- `files.by_database`

## 7. cURL examples

Upload:

```bash
curl -X POST "http://localhost:8080/api/db/mydb/files" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -F "file=@./avatar.png" \
  -F "folder_path=avatars/users" \
  -F "metadata={\"source\":\"onboarding\"}" \
  -F "expires_in=3600"
```

List:

```bash
curl "http://localhost:8080/api/db/mydb/files?limit=50&offset=0&folder_prefix=avatars" \
  -H "Authorization: Bearer $SERVICE_SECRET"
```

Download:

```bash
curl -L "http://localhost:8080/api/db/mydb/files/<file-id>" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -o downloaded.bin
```

Bulk delete:

```bash
curl -X POST "http://localhost:8080/api/db/mydb/files/bulk-delete" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"file_ids":["id1","id2"]}'
```

Create presigned URL:

```bash
curl -X POST "http://localhost:8080/api/db/mydb/files/<file-id>/presign" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"expires_in":1800,"disposition":"inline"}'
```

Batch presign URLs:

```bash
curl -X POST "http://localhost:8080/api/db/mydb/files/presign/batch" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"file_ids":["id1","id2","id3"],"expires_in":1800}'
```

Create file access token:

```bash
curl -X POST "http://localhost:8080/api/db/mydb/tokens/files" \
  -H "Authorization: Bearer $SERVICE_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"scope":"files:read","expires_in":2592000,"description":"Dashboard access"}'
```

Download file with access token (query parameter):

```bash
curl -L "http://localhost:8080/api/db/mydb/files/<file-id>?token=<access-token>" \
  -o downloaded.bin
```

Download file with access token (header):

```bash
curl -L "http://localhost:8080/api/db/mydb/files/<file-id>" \
  -H "Authorization: Bearer <access-token>" \
  -o downloaded.bin
```

## 8. Implementation notes

- The route flag `ENABLE_FILE_STORAGE` is defined in environment/config, but route-level hard gating is not currently enforced in API handlers.
- `ENABLE_FILE_PROXY_DELIVERY` is actively used by download route behavior.

## 9. Should this be a separate package?

Short answer: no, not as a separate package right now.

Recommendation:

- Extend `packages/sqlite-hub-client` with file APIs (for example `db.files.upload/list/getMeta/download/delete/bulkDelete`)
- Keep one SDK entrypoint so DB and file operations share auth, retries, and transport config
- Only split into a separate package later if file APIs need different runtime constraints or independent release cadence

This keeps developer experience simple while matching your current `connect()` model.
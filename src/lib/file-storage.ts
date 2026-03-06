import Database from "better-sqlite3";
import { createHash, randomUUID } from "crypto";
import fs from "fs";
import path from "path";

const DATA_PATH = process.env.DATA_PATH ?? "/data";
const FILES_ROOT = path.join(DATA_PATH, "files");
const BLOBS_ROOT = path.join(FILES_ROOT, "blobs");
const METADATA_DB_PATH = path.join(FILES_ROOT, "metadata.db");

const LIMITS = {
  maxFileSizeBytes: Number.parseInt(process.env.FILE_MAX_SIZE_BYTES ?? `${100 * 1024 * 1024}`, 10),
  maxFilesPerDb: Number.parseInt(process.env.FILE_MAX_FILES_PER_DB ?? "10000", 10),
  maxStoragePerDbBytes: Number.parseInt(
    process.env.FILE_MAX_STORAGE_PER_DB_BYTES ?? `${5 * 1024 * 1024 * 1024}`,
    10
  ),
  maxFilenameLength: Number.parseInt(process.env.FILE_MAX_FILENAME_LENGTH ?? "255", 10),
  maxMetadataBytes: Number.parseInt(process.env.FILE_MAX_METADATA_BYTES ?? "4096", 10),
};

const ALLOWED_MIME_PATTERNS = (process.env.FILE_ALLOWED_MIME_PATTERNS ??
  "image/*,video/*,audio/*,application/pdf,application/json,application/zip,application/gzip,text/*")
  .split(",")
  .map((value) => value.trim())
  .filter(Boolean);

let _fileDb: Database.Database | null = null;

export interface StoredFileRecord {
  id: string;
  db_name: string;
  content_hash: string;
  filename: string;
  folder_path: string;
  content_type: string | null;
  size_bytes: number;
  storage_path: string;
  uploaded_at: string;
  expires_at: string | null;
  metadata: string | null;
}

export interface UploadInput {
  dbName: string;
  filename: string;
  folderPath?: string;
  conflictMode?: "replace" | "error";
  contentType: string | null;
  bytes: Buffer;
  expiresAt: string | null;
  metadata: Record<string, unknown> | null;
}

export interface UploadResult {
  id: string;
  filename: string;
  folderPath: string;
  contentType: string | null;
  sizeBytes: number;
  contentHash: string;
  uploadedAt: string;
  expiresAt: string | null;
}

export interface ListFilesResult {
  files: StoredFileRecord[];
  total: number;
  offset: number;
  limit: number;
}

export interface FileStorageMetrics {
  totalFiles: number;
  totalBytes: number;
  byDatabase: Record<string, { files: number; bytes: number }>;
}

export class FileStorageError extends Error {
  status: number;

  constructor(message: string, status = 400) {
    super(message);
    this.status = status;
    this.name = "FileStorageError";
  }
}

function getDefaultConflictMode(): "replace" | "error" {
  const raw = (process.env.FILE_UPLOAD_CONFLICT_MODE ?? "replace").trim().toLowerCase();
  return raw === "error" ? "error" : "replace";
}

function normalizeConflictMode(input: string | undefined): "replace" | "error" {
  if (!input) return getDefaultConflictMode();
  const value = input.trim().toLowerCase();
  if (value === "replace" || value === "error") return value;
  throw new FileStorageError("conflict_mode must be 'replace' or 'error'", 400);
}

function ensureFileStorageDirs(): void {
  fs.mkdirSync(BLOBS_ROOT, { recursive: true });
}

function getFileDb(): Database.Database {
  if (_fileDb) return _fileDb;

  ensureFileStorageDirs();
  _fileDb = new Database(METADATA_DB_PATH);
  _fileDb.pragma("journal_mode = WAL");
  _fileDb.pragma("busy_timeout = 10000");
  _fileDb.exec(`
    CREATE TABLE IF NOT EXISTS files (
      id           TEXT PRIMARY KEY,
      db_name      TEXT NOT NULL,
      content_hash TEXT NOT NULL,
      filename     TEXT NOT NULL,
      folder_path  TEXT NOT NULL DEFAULT '',
      content_type TEXT,
      size_bytes   INTEGER NOT NULL,
      storage_path TEXT NOT NULL,
      uploaded_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
      expires_at   DATETIME,
      metadata     TEXT
    );

    CREATE INDEX IF NOT EXISTS idx_files_db_name ON files(db_name);
    CREATE INDEX IF NOT EXISTS idx_files_content_hash ON files(content_hash);
    CREATE INDEX IF NOT EXISTS idx_files_expires_at ON files(expires_at);
    CREATE INDEX IF NOT EXISTS idx_files_db_folder ON files(db_name, folder_path);

    CREATE TABLE IF NOT EXISTS blob_refs (
      content_hash TEXT PRIMARY KEY,
      ref_count    INTEGER NOT NULL DEFAULT 1,
      first_seen   DATETIME DEFAULT CURRENT_TIMESTAMP
    );
  `);

  const columns = _fileDb
    .prepare("PRAGMA table_info(files)")
    .all() as { name: string }[];

  if (!columns.some((c) => c.name === "folder_path")) {
    _fileDb.exec("ALTER TABLE files ADD COLUMN folder_path TEXT NOT NULL DEFAULT ''");
  }

  _fileDb.exec("CREATE INDEX IF NOT EXISTS idx_files_db_folder ON files(db_name, folder_path)");

  return _fileDb;
}

function normalizeFilename(input: string): string {
  const base = path.basename(input || "file").trim();
  if (!base) return "file";
  const compact = base.replace(/[\r\n\t]/g, " ").replace(/\s+/g, " ");
  return compact.slice(0, LIMITS.maxFilenameLength);
}

function normalizeFolderPath(input: string | undefined): string {
  if (!input) return "";

  const value = input
    .trim()
    .replace(/\\/g, "/")
    .replace(/\/+/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");

  if (!value) return "";

  const segments = value
    .split("/")
    .map((segment) => segment.trim())
    .filter(Boolean);

  if (segments.some((segment) => segment === "." || segment === "..")) {
    throw new FileStorageError("folder_path cannot contain relative segments", 400);
  }

  const normalized = segments.join("/");
  if (normalized.length > 512) {
    throw new FileStorageError("folder_path is too long (max 512 chars)", 400);
  }

  return normalized;
}

function metadataToString(metadata: Record<string, unknown> | null): string | null {
  if (!metadata) return null;
  const raw = JSON.stringify(metadata);
  if (Buffer.byteLength(raw, "utf8") > LIMITS.maxMetadataBytes) {
    throw new FileStorageError(`metadata is too large (max ${LIMITS.maxMetadataBytes} bytes)`, 400);
  }
  return raw;
}

function isMimeAllowed(contentType: string | null): boolean {
  if (!contentType) return true;
  const value = contentType.toLowerCase();
  for (const pattern of ALLOWED_MIME_PATTERNS) {
    if (pattern.endsWith("/*")) {
      const prefix = pattern.slice(0, -1).toLowerCase();
      if (value.startsWith(prefix)) return true;
      continue;
    }
    if (value === pattern.toLowerCase()) return true;
  }
  return false;
}

function getStoragePathForHash(contentHash: string): string {
  return path.join(BLOBS_ROOT, contentHash);
}

function computeContentHash(bytes: Buffer): string {
  const hash = createHash("sha256");
  hash.update(bytes);
  return hash.digest("hex");
}

function getFileByLogicalPath(dbName: string, folderPath: string, filename: string): StoredFileRecord | null {
  const db = getFileDb();
  return (
    (db
      .prepare(
        `SELECT * FROM files
         WHERE db_name = ? AND folder_path = ? AND filename = ?
         ORDER BY uploaded_at DESC, id DESC
         LIMIT 1`
      )
      .get(dbName, folderPath, filename) as StoredFileRecord) ?? null
  );
}

function requireWithinLimits(dbName: string, fileSize: number): void {
  if (fileSize <= 0) {
    throw new FileStorageError("file is empty", 400);
  }
  if (fileSize > LIMITS.maxFileSizeBytes) {
    throw new FileStorageError(`file exceeds max size (${LIMITS.maxFileSizeBytes} bytes)`, 413);
  }

  const db = getFileDb();
  const usage = db
    .prepare(
      "SELECT COUNT(*) as file_count, COALESCE(SUM(size_bytes), 0) as total_bytes FROM files WHERE db_name = ?"
    )
    .get(dbName) as { file_count: number; total_bytes: number };

  if (usage.file_count >= LIMITS.maxFilesPerDb) {
    throw new FileStorageError(`max files per database reached (${LIMITS.maxFilesPerDb})`, 507);
  }

  if (usage.total_bytes + fileSize > LIMITS.maxStoragePerDbBytes) {
    throw new FileStorageError(`database storage quota exceeded (${LIMITS.maxStoragePerDbBytes} bytes)`, 507);
  }
}

export function uploadFile(input: UploadInput): UploadResult {
  const filename = normalizeFilename(input.filename);
  const folderPath = normalizeFolderPath(input.folderPath);
  const conflictMode = normalizeConflictMode(input.conflictMode);
  const contentType = input.contentType?.trim() || null;

  if (!isMimeAllowed(contentType)) {
    throw new FileStorageError("content type is not allowed", 400);
  }

  requireWithinLimits(input.dbName, input.bytes.length);

  const contentHash = computeContentHash(input.bytes);
  const storagePath = getStoragePathForHash(contentHash);
  const metadata = metadataToString(input.metadata);
  const db = getFileDb();
  const existing = getFileByLogicalPath(input.dbName, folderPath, filename);

  if (existing && conflictMode === "error") {
    throw new FileStorageError("file already exists in this folder", 409);
  }

  if (!fs.existsSync(storagePath)) {
    fs.writeFileSync(storagePath, input.bytes);
  }

  let shouldDeleteOldBlob = false;
  let oldBlobPathToDelete: string | null = null;

  if (existing) {
    const tx = db.transaction(() => {
      if (existing.content_hash !== contentHash) {
        const newRef = db
          .prepare("SELECT ref_count FROM blob_refs WHERE content_hash = ?")
          .get(contentHash) as { ref_count: number } | undefined;

        if (newRef) {
          db.prepare("UPDATE blob_refs SET ref_count = ref_count + 1 WHERE content_hash = ?").run(contentHash);
        } else {
          db.prepare("INSERT INTO blob_refs (content_hash, ref_count) VALUES (?, 1)").run(contentHash);
        }

        const oldRef = db
          .prepare("SELECT ref_count FROM blob_refs WHERE content_hash = ?")
          .get(existing.content_hash) as { ref_count: number } | undefined;

        if (!oldRef || oldRef.ref_count <= 1) {
          db.prepare("DELETE FROM blob_refs WHERE content_hash = ?").run(existing.content_hash);
          shouldDeleteOldBlob = true;
          oldBlobPathToDelete = existing.storage_path;
        } else {
          db.prepare("UPDATE blob_refs SET ref_count = ref_count - 1 WHERE content_hash = ?").run(existing.content_hash);
        }
      }

      db.prepare(
        `UPDATE files
         SET content_hash = ?,
             content_type = ?,
             size_bytes = ?,
             storage_path = ?,
             uploaded_at = CURRENT_TIMESTAMP,
             expires_at = ?,
             metadata = ?
         WHERE id = ?`
      ).run(contentHash, contentType, input.bytes.length, storagePath, input.expiresAt, metadata, existing.id);
    });

    tx();

    if (shouldDeleteOldBlob && oldBlobPathToDelete && fs.existsSync(oldBlobPathToDelete)) {
      fs.unlinkSync(oldBlobPathToDelete);
    }

    const row = db.prepare("SELECT * FROM files WHERE id = ?").get(existing.id) as StoredFileRecord;
    return {
      id: row.id,
      filename: row.filename,
      folderPath: row.folder_path,
      contentType: row.content_type,
      sizeBytes: row.size_bytes,
      contentHash: row.content_hash,
      uploadedAt: row.uploaded_at,
      expiresAt: row.expires_at,
    };
  }

  const id = randomUUID();
  const tx = db.transaction(() => {
    const existingRef = db
      .prepare("SELECT ref_count FROM blob_refs WHERE content_hash = ?")
      .get(contentHash) as { ref_count: number } | undefined;

    if (existingRef) {
      db.prepare("UPDATE blob_refs SET ref_count = ref_count + 1 WHERE content_hash = ?").run(contentHash);
    } else {
      db.prepare("INSERT INTO blob_refs (content_hash, ref_count) VALUES (?, 1)").run(contentHash);
    }

    db.prepare(
      `INSERT INTO files (id, db_name, content_hash, filename, folder_path, content_type, size_bytes, storage_path, expires_at, metadata)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
    ).run(
      id,
      input.dbName,
      contentHash,
      filename,
      folderPath,
      contentType,
      input.bytes.length,
      storagePath,
      input.expiresAt,
      metadata
    );
  });

  tx();

  const row = db.prepare("SELECT * FROM files WHERE id = ?").get(id) as StoredFileRecord;
  return {
    id: row.id,
    filename: row.filename,
    folderPath: row.folder_path,
    contentType: row.content_type,
    sizeBytes: row.size_bytes,
    contentHash: row.content_hash,
    uploadedAt: row.uploaded_at,
    expiresAt: row.expires_at,
  };
}

function escapeSqlLike(value: string): string {
  return value.replace(/[\\%_]/g, (char) => `\\${char}`);
}

export function listFiles(
  dbName: string,
  options: { limit: number; offset: number; sort: string; order: "asc" | "desc"; folderPrefix?: string }
): ListFilesResult {
  const db = getFileDb();
  const safeSort = ["uploaded_at", "size_bytes", "filename", "folder_path"].includes(options.sort)
    ? options.sort
    : "uploaded_at";
  const safeOrder = options.order === "asc" ? "ASC" : "DESC";
  const folderPrefix = normalizeFolderPath(options.folderPrefix);

  if (!folderPrefix) {
    const files = db
      .prepare(
        `SELECT * FROM files WHERE db_name = ? ORDER BY ${safeSort} ${safeOrder} LIMIT ? OFFSET ?`
      )
      .all(dbName, options.limit, options.offset) as StoredFileRecord[];

    const totalRow = db
      .prepare("SELECT COUNT(*) as count FROM files WHERE db_name = ?")
      .get(dbName) as { count: number };

    return {
      files,
      total: totalRow.count,
      offset: options.offset,
      limit: options.limit,
    };
  }

  const likePrefix = `${escapeSqlLike(folderPrefix)}/%`;

  const files = db
    .prepare(
      `SELECT * FROM files
       WHERE db_name = ?
         AND (folder_path = ? OR folder_path LIKE ? ESCAPE '\\')
       ORDER BY ${safeSort} ${safeOrder}
       LIMIT ? OFFSET ?`
    )
    .all(dbName, folderPrefix, likePrefix, options.limit, options.offset) as StoredFileRecord[];

  const totalRow = db
    .prepare(
      `SELECT COUNT(*) as count FROM files
       WHERE db_name = ?
         AND (folder_path = ? OR folder_path LIKE ? ESCAPE '\\')`
    )
    .get(dbName, folderPrefix, likePrefix) as { count: number };

  return {
    files,
    total: totalRow.count,
    offset: options.offset,
    limit: options.limit,
  };
}

export function getFileById(dbName: string, fileId: string): StoredFileRecord | null {
  const db = getFileDb();
  return (db.prepare("SELECT * FROM files WHERE id = ? AND db_name = ?").get(fileId, dbName) as StoredFileRecord) ?? null;
}

export function deleteFile(dbName: string, fileId: string): boolean {
  const db = getFileDb();
  const record = getFileById(dbName, fileId);
  if (!record) return false;

  let shouldDeleteBlob = false;
  const tx = db.transaction(() => {
    db.prepare("DELETE FROM files WHERE id = ? AND db_name = ?").run(fileId, dbName);

    const refRow = db
      .prepare("SELECT ref_count FROM blob_refs WHERE content_hash = ?")
      .get(record.content_hash) as { ref_count: number } | undefined;

    if (!refRow) {
      shouldDeleteBlob = true;
      return;
    }

    if (refRow.ref_count <= 1) {
      db.prepare("DELETE FROM blob_refs WHERE content_hash = ?").run(record.content_hash);
      shouldDeleteBlob = true;
    } else {
      db.prepare("UPDATE blob_refs SET ref_count = ref_count - 1 WHERE content_hash = ?").run(record.content_hash);
    }
  });

  tx();

  if (shouldDeleteBlob && fs.existsSync(record.storage_path)) {
    fs.unlinkSync(record.storage_path);
  }

  return true;
}

export function deleteFilesForDatabase(dbName: string): { deleted: number } {
  const db = getFileDb();
  const rows = db.prepare("SELECT id FROM files WHERE db_name = ?").all(dbName) as { id: string }[];
  let deleted = 0;
  for (const row of rows) {
    if (deleteFile(dbName, row.id)) deleted += 1;
  }
  return { deleted };
}

export function bulkDeleteFiles(dbName: string, ids: string[]): { deleted: number; failed: number } {
  let deleted = 0;
  let failed = 0;

  for (const id of ids) {
    try {
      if (deleteFile(dbName, id)) deleted += 1;
      else failed += 1;
    } catch {
      failed += 1;
    }
  }

  return { deleted, failed };
}

export function isFileExpired(record: StoredFileRecord): boolean {
  if (!record.expires_at) return false;
  const expiresAt = Date.parse(record.expires_at);
  if (Number.isNaN(expiresAt)) return false;
  return Date.now() > expiresAt;
}

export function getBlobAbsolutePath(contentHash: string): string {
  return getStoragePathForHash(contentHash);
}

export function getFileStorageMetrics(): FileStorageMetrics {
  const db = getFileDb();
  const byDbRows = db
    .prepare(
      "SELECT db_name, COUNT(*) as files, COALESCE(SUM(size_bytes), 0) as bytes FROM files GROUP BY db_name"
    )
    .all() as { db_name: string; files: number; bytes: number }[];

  const totals = db
    .prepare("SELECT COUNT(*) as files, COALESCE(SUM(size_bytes), 0) as bytes FROM files")
    .get() as { files: number; bytes: number };

  const byDatabase: Record<string, { files: number; bytes: number }> = {};
  for (const row of byDbRows) {
    byDatabase[row.db_name] = { files: row.files, bytes: row.bytes };
  }

  return {
    totalFiles: totals.files,
    totalBytes: totals.bytes,
    byDatabase,
  };
}

import type { HttpAdapterOptions } from "./adapters/http.js";

export interface FileListOptions {
  limit?: number;
  offset?: number;
  sort?: "uploaded_at" | "size_bytes" | "filename" | "folder_path";
  order?: "asc" | "desc";
  folderPrefix?: string;
}

export interface StoredFile {
  id: string;
  db_name: string;
  content_hash: string;
  filename: string;
  folder_path: string;
  content_type: string | null;
  size_bytes: number;
  storage_path?: string;
  uploaded_at: string;
  expires_at: string | null;
  metadata: string | null;
}

export interface FileListResponse {
  files: StoredFile[];
  total: number;
  offset: number;
  limit: number;
}

export interface UploadFileInput {
  file: Blob | Uint8Array | ArrayBuffer;
  filename?: string;
  folderPath?: string;
  conflictMode?: "replace" | "error";
  contentType?: string;
  metadata?: Record<string, unknown>;
  expiresIn?: number;
}

export interface UploadFileResponse {
  id: string;
  filename: string;
  folder_path: string;
  size_bytes: number;
  content_type: string | null;
  url: string;
  uploaded_at: string;
  expires_at: string | null;
}

export interface FileMetaResponse {
  id: string;
  filename: string;
  folder_path: string;
  content_type: string | null;
  size_bytes: number;
  uploaded_at: string;
  expires_at: string | null;
  metadata: Record<string, unknown> | null;
  content_hash: string;
}

export interface BulkDeleteFilesResponse {
  deleted: number;
  failed: number;
}

export interface PresignFileOptions {
  expiresIn?: number;
  disposition?: "inline" | "attachment";
}

export interface PresignFileResponse {
  url: string;
  expires_at: string;
  expires_in: number;
  disposition: "inline" | "attachment";
  token_type: "signed_query";
}

export interface BatchPresignOptions {
  fileIds: string[];
  expiresIn?: number;
  disposition?: "inline" | "attachment";
}

export interface BatchPresignResult {
  file_id: string;
  url?: string;
  expires_at?: string;
  expires_in?: number;
  disposition?: "inline" | "attachment";
  error?: string;
}

export interface BatchPresignResponse {
  results: BatchPresignResult[];
  token_type: "signed_query";
  total: number;
  successful: number;
  failed: number;
}

export interface CreateFileAccessTokenOptions {
  scope?: "files:read";
  expiresIn?: number;
  description?: string;
}

export interface FileAccessTokenResponse {
  token: string;
  token_type: "bearer";
  expires_at: string;
  expires_in: number;
  scope: string;
  description?: string;
  usage: string;
}

function toBlob(file: UploadFileInput["file"], contentType?: string): Blob {
  if (file instanceof Blob) return file;
  if (file instanceof Uint8Array) return new Blob([file], { type: contentType });
  return new Blob([new Uint8Array(file)], { type: contentType });
}

export class FileClient {
  private readonly baseUrl: string;
  private readonly basePath: string;

  constructor(private readonly options: HttpAdapterOptions) {
    this.baseUrl = options.url.replace(/\/$/, "");
    this.basePath = `/api/db/${encodeURIComponent(options.db)}/files`;
  }

  private async requestJson<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers ?? {});
    headers.set("Authorization", `Bearer ${this.options.token}`);

    const res = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers,
    });

    if (res.status === 204) {
      return undefined as T;
    }

    const body = (await res.json().catch(() => null)) as { error?: string } | null;
    if (!res.ok) {
      throw new Error(body?.error ?? `sqlite-hub: HTTP ${res.status} (db: "${this.options.db}")`);
    }

    return body as T;
  }

  async list(options: FileListOptions = {}): Promise<FileListResponse> {
    const qs = new URLSearchParams();
    if (options.limit !== undefined) qs.set("limit", String(options.limit));
    if (options.offset !== undefined) qs.set("offset", String(options.offset));
    if (options.sort) qs.set("sort", options.sort);
    if (options.order) qs.set("order", options.order);
    if (options.folderPrefix) qs.set("folder_prefix", options.folderPrefix);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return this.requestJson<FileListResponse>(`${this.basePath}${suffix}`);
  }

  async upload(input: UploadFileInput): Promise<UploadFileResponse> {
    const formData = new FormData();
    const blob = toBlob(input.file, input.contentType);
    const filename = input.filename ?? "file";
    formData.append("file", blob, filename);

    if (input.filename) formData.append("filename", input.filename);
    if (input.folderPath) formData.append("folder_path", input.folderPath);
    if (input.conflictMode) formData.append("conflict_mode", input.conflictMode);
    if (input.contentType) formData.append("content_type", input.contentType);
    if (input.metadata) formData.append("metadata", JSON.stringify(input.metadata));
    if (input.expiresIn !== undefined) formData.append("expires_in", String(input.expiresIn));

    const headers = new Headers({ Authorization: `Bearer ${this.options.token}` });
    const res = await fetch(`${this.baseUrl}${this.basePath}`, {
      method: "POST",
      headers,
      body: formData,
    });

    const body = (await res.json().catch(() => null)) as
      | (UploadFileResponse & { error?: string })
      | null;

    if (!res.ok || !body) {
      throw new Error(body?.error ?? `sqlite-hub: HTTP ${res.status} (db: "${this.options.db}")`);
    }

    return body;
  }

  async getMeta(fileId: string): Promise<FileMetaResponse> {
    return this.requestJson<FileMetaResponse>(`${this.basePath}/${encodeURIComponent(fileId)}/meta`);
  }

  async delete(fileId: string): Promise<void> {
    await this.requestJson<void>(`${this.basePath}/${encodeURIComponent(fileId)}`, {
      method: "DELETE",
    });
  }

  async bulkDelete(fileIds: string[]): Promise<BulkDeleteFilesResponse> {
    return this.requestJson<BulkDeleteFilesResponse>(`${this.basePath}/bulk-delete`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ file_ids: fileIds }),
    });
  }

  async presign(fileId: string, options: PresignFileOptions = {}): Promise<PresignFileResponse> {
    return this.requestJson<PresignFileResponse>(`${this.basePath}/${encodeURIComponent(fileId)}/presign`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        ...(options.expiresIn !== undefined ? { expires_in: options.expiresIn } : {}),
        ...(options.disposition ? { disposition: options.disposition } : {}),
      }),
    });
  }

  async batchPresign(options: BatchPresignOptions): Promise<BatchPresignResponse> {
    return this.requestJson<BatchPresignResponse>(`${this.basePath}/presign/batch`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        file_ids: options.fileIds,
        ...(options.expiresIn !== undefined ? { expires_in: options.expiresIn } : {}),
        ...(options.disposition ? { disposition: options.disposition } : {}),
      }),
    });
  }

  async createFileAccessToken(options: CreateFileAccessTokenOptions = {}): Promise<FileAccessTokenResponse> {
    const tokenPath = `/api/db/${encodeURIComponent(this.options.db)}/tokens/files`;
    return this.requestJson<FileAccessTokenResponse>(tokenPath, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        scope: "files:read",
        ...(options.expiresIn !== undefined ? { expires_in: options.expiresIn } : {}),
        ...(options.description ? { description: options.description } : {}),
      }),
    });
  }

  getDownloadUrl(fileId: string): string {
    return `${this.baseUrl}${this.basePath}/${encodeURIComponent(fileId)}`;
  }
}
import pingpong from "@pingpong-js/fetch";
import type { IAdapter, RawResult } from "./types.js";

export interface HttpAdapterOptions {
  /** Base URL of the sqlite-hub deployment, e.g. https://my-app.up.railway.app */
  url: string;
  /**
   * Per-DB `service_secret` for this database.
   * Generate one in the sqlite-hub admin dashboard under Settings → Service secret.
   * Sent as `Authorization: Bearer <token>` on every request.
   */
  token: string;
  /** Name of the database to operate on */
  db: string;
  /** Request timeout in ms (default: 10 000) */
  timeout?: number;
}

/**
 * Adapter that executes SQL via the sqlite-hub HTTP API
 * (POST /api/db/:name/exec).
 */
export class HttpAdapter implements IAdapter {
  private readonly http: ReturnType<typeof pingpong.create>;
  private readonly dbPath: string;

  constructor(private readonly options: HttpAdapterOptions) {
    this.dbPath = `/api/db/${encodeURIComponent(options.db)}/exec`;
    this.http = pingpong.create({
      baseURL: options.url.replace(/\/$/, ""),
      timeout: options.timeout ?? 10_000,
      headers: {
        Authorization: `Bearer ${options.token}`,
        "Content-Type": "application/json",
      },
    });
  }

  async exec<T = Record<string, unknown>>(
    sql: string,
    bindings?: unknown[]
  ): Promise<RawResult<T>> {
    const res = await this.http.post(this.dbPath, {
      sql,
      ...(bindings?.length ? { bindings } : {}),
    });

    const body = res.data as (RawResult<T> & { error?: string }) | null;

    if (res.isError() || body == null) {
      throw new Error(
        (body as { error?: string } | null)?.error ??
          `sqlite-hub: HTTP ${res.status} — no response body (db: "${this.options.db}")`
      );
    }

    return body;
  }
}

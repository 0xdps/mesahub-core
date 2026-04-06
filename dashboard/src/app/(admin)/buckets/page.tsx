export const dynamic = "force-dynamic";

import { goFetchAdmin } from "@/lib/go-api";
import Link from "next/link";

interface BucketRow {
  id: string;
  user_id: string;
  name: string;
  display_name: string;
  description: string | null;
  instance_id: string | null;
  status: string;
  size_bytes: number;
  created_at: string;
}

interface QueryResponse {
  rows?: Record<string, unknown>[];
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

async function fetchBuckets(): Promise<BucketRow[]> {
  const res = await goFetchAdmin("/api/system/db/control/query", {
    method: "POST",
    body: JSON.stringify({
      sql: `SELECT b.id, b.user_id, b.name, b.display_name, b.description,
                   b.instance_id, b.status, b.size_bytes, b.created_at
            FROM buckets b
            WHERE b.status != 'deleted'
            ORDER BY b.created_at DESC
            LIMIT 500`,
    }),
  }).catch(() => null);

  if (!res?.ok) return [];
  const data: QueryResponse = await res.json().catch(() => ({}));
  // The system query endpoint returns { rows: [...] } with column-keyed objects
  return (data.rows ?? []) as unknown as BucketRow[];
}

export default async function BucketsAdminPage() {
  const buckets = await fetchBuckets();
  const activeBuckets = buckets.filter((b) => b.status === "active");

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full overflow-auto">
      <div className="flex items-center justify-between mb-8">
        <div>
          <div className="flex items-center gap-3 mb-1">
            <Link href="/" className="text-sm text-neutral-400 hover:text-white">
              ← Databases
            </Link>
          </div>
          <h1 className="text-xl font-semibold">Buckets</h1>
          <p className="text-sm text-neutral-400 mt-1">
            {activeBuckets.length} active bucket{activeBuckets.length !== 1 ? "s" : ""}{" "}
            across all users
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            href="/buckets/new"
            className="text-sm bg-white text-black px-3 py-1.5 rounded font-medium hover:bg-neutral-200 transition-colors"
          >
            + New bucket
          </Link>
          <Link
            href="/db/new"
            className="text-sm text-neutral-200 hover:text-white px-3 py-1.5 rounded border border-neutral-700 hover:border-neutral-500 transition-colors"
          >
            + New database
          </Link>
        </div>
      </div>

      {buckets.length === 0 ? (
        <div className="rounded-lg border border-neutral-800 p-8 text-center">
          <p className="text-sm text-neutral-400">
            No buckets yet.
          </p>
          <Link
            href="/buckets/new"
            className="inline-block mt-3 text-sm text-white underline"
          >
            Create a bucket
          </Link>
        </div>
      ) : (
        <div className="rounded-lg border border-neutral-800 overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-neutral-800 text-neutral-400 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-3">Display name</th>
                <th className="text-left px-4 py-3">Internal name</th>
                <th className="text-left px-4 py-3">User</th>
                <th className="text-left px-4 py-3">Status</th>
                <th className="text-right px-4 py-3">Size</th>
                <th className="text-right px-4 py-3">Created</th>
                <th className="text-right px-4 py-3">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {buckets.map((bucket) => (
                <tr key={bucket.id} className="hover:bg-neutral-800/40 transition-colors">
                  <td className="px-4 py-3">
                    <span className="font-medium text-white">{bucket.display_name}</span>
                    {bucket.description && (
                      <p className="text-xs text-neutral-500 mt-0.5 truncate max-w-[200px]">
                        {bucket.description}
                      </p>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <code className="text-xs text-neutral-300 font-mono">{bucket.name}</code>
                  </td>
                  <td className="px-4 py-3">
                    <code className="text-xs text-neutral-400 font-mono truncate max-w-[100px] block">
                      {bucket.user_id}
                    </code>
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                        bucket.status === "active"
                          ? "bg-green-900/40 text-green-400 border border-green-800/50"
                          : "bg-neutral-800 text-neutral-400"
                      }`}
                    >
                      {bucket.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right text-neutral-300 font-mono text-xs">
                    {formatBytes(bucket.size_bytes ?? 0)}
                  </td>
                  <td className="px-4 py-3 text-right text-neutral-500 text-xs tabular-nums">
                    {new Date(bucket.created_at + "Z").toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      href={`/buckets/${encodeURIComponent(bucket.name)}/files`}
                      className="text-xs text-emerald-400 hover:text-emerald-300"
                    >
                      Files →
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <p className="mt-6 text-xs text-neutral-600">
        Bucket files are stored under the <code className="text-neutral-500">bkt-{"{name}"}</code> namespace in the shared file storage.
        Use the API with an <code className="text-neutral-500">shs_</code> key scoped to{" "}
        <code className="text-neutral-500">bucket:*</code> or <code className="text-neutral-500">bucket:{"<name>"}</code> to access files.
      </p>
    </div>
  );
}

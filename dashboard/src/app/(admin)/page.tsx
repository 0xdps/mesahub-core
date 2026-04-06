export const dynamic = "force-dynamic";

import { goFetchAdmin } from "@/lib/go-api";
import Link from "next/link";

interface DbRecord {
  id: number;
  name: string;
  owner: string;
  status: string;
  created_at: string;
  size_bytes?: number;
}

interface MetricsResponse {
  volume_used_bytes: number;
  volume_total_bytes: number;
  volume_used_percent: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

export default async function DashboardPage() {
  const [dbsRes, metricsRes] = await Promise.all([
    goFetchAdmin("/api/db").catch(() => null),
    goFetchAdmin("/api/metrics").catch(() => null),
  ]);

  const dbs: DbRecord[] = dbsRes?.ok ? await dbsRes.json() : [];
  const metrics: MetricsResponse | null = metricsRes?.ok ? await metricsRes.json() : null;

  const SYSTEM_DBS = new Set(["registry", "control"]);
  const userDbs = dbs.filter((d) => !SYSTEM_DBS.has(d.name));
  const activeDbs = userDbs.filter((d) => d.status === "active");
  const volume = metrics
    ? {
        used: metrics.volume_used_bytes,
        total: metrics.volume_total_bytes,
        usedPercent: metrics.volume_used_percent,
      }
    : null;

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-xl font-semibold">Databases</h1>
          <p className="text-sm text-neutral-400 mt-1">
            {activeDbs.length} active database{activeDbs.length !== 1 ? "s" : ""}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            href="/buckets"
            className="text-sm text-neutral-200 hover:text-white px-3 py-1.5 rounded border border-neutral-700 hover:border-neutral-500 transition-colors"
          >
            + New bucket
          </Link>
          <Link
            href="/db/new"
            className="text-sm bg-white text-black px-3 py-1.5 rounded font-medium hover:bg-neutral-200 transition-colors"
          >
            + New database
          </Link>
        </div>
      </div>

      {/* Volume usage */}
      {volume && (
      <div className="mb-8 rounded-lg border border-neutral-800 p-4">
        <div className="flex items-center justify-between text-sm mb-2">
          <span className="text-neutral-400">Volume usage</span>
          <span className={volume.usedPercent > 80 ? "text-red-400" : "text-neutral-300"}>
            {volume.usedPercent}% — {formatBytes(volume.used)} / {formatBytes(volume.total)}
          </span>
        </div>
        <div className="h-1.5 bg-neutral-800 rounded-full overflow-hidden">
          <div
            className={`h-full rounded-full ${volume.usedPercent > 80 ? "bg-red-500" : "bg-blue-500"}`}
            style={{ width: `${Math.min(volume.usedPercent, 100)}%` }}
          />
        </div>
      </div>
      )}

      {/* System databases */}
      <div className="mb-8">
        <h2 className="text-sm font-medium text-neutral-400 uppercase tracking-wide mb-3">System databases</h2>
        <div className="rounded-lg border border-neutral-800 overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-neutral-800 text-neutral-400 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-3">Name</th>
                <th className="text-left px-4 py-3">Description</th>
                <th className="text-right px-4 py-3">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {(["registry", "control"] as const).map((sysName) => (
                <tr key={sysName} className="hover:bg-neutral-800/40 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-white">{sysName}</span>
                      <span className="inline-block px-1.5 py-0.5 rounded text-xs font-medium bg-amber-900/50 text-amber-400 border border-amber-800/50">
                        system
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-neutral-500 text-xs">
                    {sysName === "registry"
                      ? "Stores all database records, service secrets, and metadata"
                      : "Stores user accounts, API keys, and control-plane data"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      href={`/db/system/${sysName}`}
                      className="text-xs text-blue-400 hover:text-blue-300"
                    >
                      Browse →
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Deleted databases link */}
      <div className="mb-8">
        <Link
          href="/deleted"
          className="text-sm text-neutral-400 hover:text-white transition-colors"
        >
          View deleted databases →
        </Link>
      </div>

      {/* DB table */}
      {userDbs.length === 0 ? (
        <div className="text-center py-16 text-neutral-500 text-sm">
          No databases yet.{" "}
          <Link href="/db/new" className="text-white underline">
            Register one
          </Link>
          .
        </div>
      ) : (
        <div className="rounded-lg border border-neutral-800 overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-neutral-800 text-neutral-400 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-3">Name</th>
                <th className="text-left px-4 py-3">Owner</th>
                <th className="text-left px-4 py-3">Size</th>
                <th className="text-left px-4 py-3">Created</th>
                <th className="text-left px-4 py-3">Status</th>
                <th className="text-right px-4 py-3">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {userDbs.map((db) => (
                <tr key={db.id} className="hover:bg-neutral-800/40 transition-colors">
                  <td className="px-4 py-3 font-mono text-white">{db.name}</td>
                  <td className="px-4 py-3 text-neutral-300">{db.owner}</td>
                  <td className="px-4 py-3 text-neutral-400">{db.size_bytes != null ? formatBytes(db.size_bytes) : "—"}</td>
                  <td className="px-4 py-3 text-neutral-400">
                    {new Date(db.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`inline-block px-2 py-0.5 rounded-full text-xs font-medium ${
                        db.status === "active"
                          ? "bg-green-900 text-green-300"
                          : "bg-neutral-800 text-neutral-400"
                      }`}
                    >
                      {db.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-3">
                      <Link
                        href={`/db/${db.name}/settings`}
                        className="text-xs text-neutral-400 hover:text-white"
                      >
                        Settings
                      </Link>
                      <Link
                        href={`/db/${db.name}`}
                        className="text-xs text-blue-400 hover:text-blue-300"
                      >
                        Browse →
                      </Link>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

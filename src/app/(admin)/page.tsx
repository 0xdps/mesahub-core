export const dynamic = "force-dynamic";

import { getDbPath, getFileSizeBytes, getVolumeSummary } from "@/lib/fs";
import { listDatabases } from "@/lib/registry";
import Link from "next/link";

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

const FILE_STORAGE_ENABLED =
  (process.env.ENABLE_FILE_STORAGE ?? "false").toLowerCase() === "true";

export default function DashboardPage() {
  const dbs = listDatabases().map((d) => ({
    ...d,
    size_bytes: getFileSizeBytes(getDbPath(d.name)),
  }));
  const volume = getVolumeSummary();
  const activeDbs = dbs.filter((d) => d.status === "active");

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-xl font-semibold">Databases</h1>
          <p className="text-sm text-neutral-400 mt-1">
            {activeDbs.length} active database{activeDbs.length !== 1 ? "s" : ""}
          </p>
        </div>
        <Link
          href="/db/new"
          className="text-sm bg-white text-black px-3 py-1.5 rounded font-medium hover:bg-neutral-200 transition-colors"
        >
          + New database
        </Link>
      </div>

      {/* Volume usage */}
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
      {dbs.length === 0 ? (
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
              {dbs.map((db) => (
                <tr key={db.id} className="hover:bg-neutral-800/40 transition-colors">
                  <td className="px-4 py-3 font-mono text-white">{db.name}</td>
                  <td className="px-4 py-3 text-neutral-300">{db.owner}</td>
                  <td className="px-4 py-3 text-neutral-400">{formatBytes(db.size_bytes)}</td>
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
                      {FILE_STORAGE_ENABLED && (
                        <Link
                          href={`/db/${db.name}/files`}
                          className="text-xs text-emerald-400 hover:text-emerald-300"
                        >
                          Files
                        </Link>
                      )}
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

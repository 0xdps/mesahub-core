"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

const FILE_STORAGE_ENABLED =
  (process.env.NEXT_PUBLIC_ENABLE_FILE_STORAGE ?? "false").toLowerCase() === "true"; // resolved at build time by vite.config.ts define

interface DBRecord {
  id: string;
  name: string;
  owner: string;
  description: string | null;
  created_at: string;
  status: string;
  size_bytes: number;
}

interface SystemDB {
  name: string;
  label: string;
  description: string;
  size_bytes: number;
}

interface Volume {
  volume_used_bytes: number;
  volume_total_bytes: number;
  volume_used_percent: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
}

export default function DashboardPage() {
  const [dbs, setDbs] = useState<DBRecord[]>([]);
  const [systemDbs, setSystemDbs] = useState<SystemDB[]>([]);
  const [volume, setVolume] = useState<Volume | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      fetch("/api/db").then((r) => r.json()),
      fetch("/api/metrics").then((r) => r.json()),
      fetch("/api/system/dbs").then((r) => r.json()),
    ]).then(([dbData, metricsData, systemData]) => {
      setDbs(Array.isArray(dbData) ? dbData : []);
      setVolume({
        volume_used_bytes: metricsData.volume_used_bytes ?? 0,
        volume_total_bytes: metricsData.volume_total_bytes ?? 0,
        volume_used_percent: metricsData.volume_used_percent ?? 0,
      });
      setSystemDbs(Array.isArray(systemData) ? systemData : []);
      setLoading(false);
    });
  }, []);

  const activeDbs = dbs.filter((d) => d.status === "active");

  if (loading) {
    return (
      <div className="px-6 py-8 text-neutral-400 text-sm">Loading…</div>
    );
  }

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-xl font-semibold">Databases</h1>
          <p className="text-sm text-neutral-400 mt-1">
            {activeDbs.length} active user database{activeDbs.length !== 1 ? "s" : ""}
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
      {volume && (
        <div className="mb-8 rounded-lg border border-neutral-800 p-4">
          <div className="flex items-center justify-between text-sm mb-2">
            <span className="text-neutral-400">Volume usage</span>
            <span
              className={volume.volume_used_percent > 80 ? "text-red-400" : "text-neutral-300"}
            >
              {volume.volume_used_percent.toFixed(1)}% —{" "}
              {formatBytes(volume.volume_used_bytes)} / {formatBytes(volume.volume_total_bytes)}
            </span>
          </div>
          <div className="h-1.5 bg-neutral-800 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full ${volume.volume_used_percent > 80 ? "bg-red-500" : "bg-blue-500"}`}
              style={{ width: `${Math.min(volume.volume_used_percent, 100)}%` }}
            />
          </div>
        </div>
      )}

      {/* Deleted databases link */}
      <div className="mb-8">
        <Link
          href="/deleted"
          className="text-sm text-neutral-400 hover:text-white transition-colors"
        >
          View deleted databases →
        </Link>
      </div>

      {/* System databases */}
      {systemDbs.length > 0 && (
        <div className="mb-10">
          <div className="flex items-center gap-2 mb-3">
            <svg width="13" height="13" viewBox="0 0 13 13" fill="none" aria-hidden="true" className="text-amber-400">
              <path d="M6.5 1.5L8 5H12L8.75 7.5L10 11L6.5 8.5L3 11L4.25 7.5L1 5H5L6.5 1.5Z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" />
            </svg>
            <h2 className="text-sm font-semibold text-neutral-300 uppercase tracking-wider">System Databases</h2>
          </div>
          <div className="rounded-lg border border-amber-900/40 overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-amber-950/30 text-neutral-400 text-xs uppercase">
                <tr>
                  <th className="text-left px-4 py-3">Name</th>
                  <th className="text-left px-4 py-3">Description</th>
                  <th className="text-left px-4 py-3">Size</th>
                  <th className="text-right px-4 py-3">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-neutral-800">
                {systemDbs.map((db) => (
                  <tr key={db.name} className="hover:bg-neutral-800/40 transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-amber-300">{db.label}</span>
                        <span className="inline-block px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-900/50 text-amber-400 border border-amber-800/50">
                          protected
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-neutral-400">{db.description}</td>
                    <td className="px-4 py-3 text-neutral-400">{formatBytes(db.size_bytes)}</td>
                    <td className="px-4 py-3 text-right">
                      <Link
                        href={`/db/_system/${db.name}`}
                        className="text-xs text-amber-400 hover:text-amber-300"
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
      )}

      {/* User DB table */}
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


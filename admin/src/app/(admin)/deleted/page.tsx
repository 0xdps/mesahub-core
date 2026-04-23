"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

interface DeletedDb {
  name: string;
  original_name: string | null;
  owner: string;
  description: string | null;
  deleted_at: string | null;
  size_bytes: number;
  exists: boolean;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr + "Z").getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export default function DeletedDatabasesPage() {
  const [databases, setDatabases] = useState<DeletedDb[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [confirmHardDelete, setConfirmHardDelete] = useState<string | null>(null);

  const loadDatabases = useCallback(() => {
    setLoading(true);
    fetch("/api/db/deleted")
      .then((r) => r.json())
      .then((data) => {
        setDatabases(data);
        setLoading(false);
      })
      .catch(() => {
        setError("Failed to load deleted databases");
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    loadDatabases();
  }, [loadDatabases]);

  async function handleRestore(name: string) {
    setActionLoading(name);
    setError("");
    const res = await fetch(`/api/db/deleted/${encodeURIComponent(name)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "restore" }),
    });
    setActionLoading(null);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to restore database");
      return;
    }
    loadDatabases();
  }

  async function handleHardDelete(name: string) {
    setActionLoading(name);
    setError("");
    const res = await fetch(`/api/db/deleted/${encodeURIComponent(name)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "hard_delete" }),
    });
    setActionLoading(null);
    setConfirmHardDelete(null);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to delete database");
      return;
    }
    loadDatabases();
  }

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
      <div className="mb-6">
        <Link
          href="/"
          className="text-sm text-neutral-400 hover:text-white"
        >
          ← Back to dashboard
        </Link>
        <h1 className="text-xl font-semibold mt-3">Deleted Databases</h1>
        <p className="text-sm text-neutral-400 mt-1">
          Databases that have been soft-deleted. You can restore them or
          permanently delete them.
        </p>
      </div>

      {error && (
        <div className="mb-4 rounded bg-red-950/50 border border-red-900/60 px-4 py-2 text-sm text-red-400">
          {error}
        </div>
      )}

      {loading ? (
        <p className="text-sm text-neutral-400">Loading…</p>
      ) : databases.length === 0 ? (
        <div className="text-center py-16">
          <p className="text-sm text-neutral-500">No deleted databases</p>
        </div>
      ) : (
        <div className="space-y-3">
          {databases.map((db) => (
            <div
              key={db.name}
              className="border border-neutral-800 rounded-lg p-4 flex items-center justify-between gap-4"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-sm text-white truncate">
                    {db.original_name ?? db.name}
                  </span>
                  <span className="shrink-0 text-xs px-1.5 py-0.5 rounded bg-red-950 text-red-400 border border-red-900/40">
                    deleted
                  </span>
                </div>
                <div className="flex items-center gap-3 mt-1 text-xs text-neutral-500">
                  <span>Owner: {db.owner}</span>
                  {db.deleted_at && <span>Deleted {timeAgo(db.deleted_at)}</span>}
                  <span>{formatBytes(db.size_bytes)}</span>
                </div>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                {confirmHardDelete === db.name ? (
                  <>
                    <span className="text-xs text-neutral-400">
                      Permanently delete?
                    </span>
                    <button
                      onClick={() => handleHardDelete(db.name)}
                      disabled={actionLoading === db.name}
                      className="text-xs px-3 py-1.5 rounded bg-red-700 text-white hover:bg-red-600 transition-colors disabled:opacity-50"
                    >
                      {actionLoading === db.name ? "Deleting…" : "Yes, delete"}
                    </button>
                    <button
                      onClick={() => setConfirmHardDelete(null)}
                      className="text-xs px-3 py-1.5 rounded border border-neutral-700 text-neutral-400 hover:text-white transition-colors"
                    >
                      Cancel
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      onClick={() => handleRestore(db.name)}
                      disabled={actionLoading === db.name}
                      className="text-xs px-3 py-1.5 rounded border border-green-900 text-green-400 hover:border-green-600 hover:text-green-300 transition-colors disabled:opacity-50"
                    >
                      {actionLoading === db.name ? "Restoring…" : "Restore"}
                    </button>
                    <button
                      onClick={() => setConfirmHardDelete(db.name)}
                      disabled={actionLoading === db.name}
                      className="text-xs px-3 py-1.5 rounded border border-red-900 text-red-400 hover:border-red-600 hover:text-red-300 transition-colors disabled:opacity-50"
                    >
                      Delete forever
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

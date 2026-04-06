"use client";

import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";

interface DbInfo {
  name: string;
  owner: string;
  description: string | null;
  status: string;
  created_at: string;
}

export default function DbSettingsPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name } = use(params);
  const router = useRouter();

  const [db, setDb] = useState<DbInfo | null>(null);
  const [pageLoading, setPageLoading] = useState(true);
  const [error, setError] = useState("");
  const [statusLoading, setStatusLoading] = useState(false);
  const [confirmDropInput, setConfirmDropInput] = useState("");
  const [dropLoading, setDropLoading] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const [resetLoading, setResetLoading] = useState(false);
  const [successMessage, setSuccessMessage] = useState<string>("");

  const loadDb = useCallback(() => {
    fetch(`/api/db/${name}`)
      .then((r) => r.json())
      .then((data) => {
        setDb(data);
        setPageLoading(false);
      })
      .catch(() => {
        setError("Failed to load database info");
        setPageLoading(false);
      });
  }, [name]);

  useEffect(() => { loadDb(); }, [loadDb]);

  async function handleDropDatabase() {
    if (confirmDropInput !== name) return;
    setDropLoading(true);
    setError("");
    const res = await fetch(`/api/db/${name}`, { method: "DELETE" });
    setDropLoading(false);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to drop database");
      return;
    }
    router.push("/");
  }

  async function handleResetDatabase() {
    setResetLoading(true);
    setError("");
    setSuccessMessage("");
    const res = await fetch(`/api/db/${name}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "reset_db" }),
    });
    setResetLoading(false);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to reset database");
      return;
    }
    setSuccessMessage("✓ Database reset successfully! All tables have been removed.");
    setConfirmReset(false);
    loadDb();
    setTimeout(() => setSuccessMessage(""), 5000);
  }

  async function handleToggleStatus() {
    if (!db) return;
    const newStatus = db.status === "active" ? "inactive" : "active";
    setStatusLoading(true);
    setError("");
    const res = await fetch(`/api/db/${name}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "set_status", status: newStatus }),
    });
    setStatusLoading(false);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to update status");
      return;
    }
    setDb((prev) => prev ? { ...prev, status: newStatus } : prev);
  }

  if (pageLoading) {
    return (
      <div className="px-6 py-8 max-w-5xl mx-auto w-full">
        <p className="text-sm text-neutral-400">Loading…</p>
      </div>
    );
  }

  if (!db) {
    return (
      <div className="px-6 py-8 max-w-5xl mx-auto w-full">
        <p className="text-sm text-red-400">{error || "Database not found"}</p>
        <Link href="/" className="text-sm text-neutral-400 hover:text-white mt-4 block">
          ← Back to dashboard
        </Link>
      </div>
    );
  }

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full h-full overflow-auto">
      <div className="max-w-lg">

        {/* Header */}
        <div className="mb-8">
          <div className="flex items-center gap-4 text-sm">
            <Link href="/" className="text-neutral-400 hover:text-white">
              ← Back to dashboard
            </Link>
            <Link href={`/db/${name}/files`} className="text-emerald-400 hover:text-emerald-300">
              Files
            </Link>
            <Link href={`/db/${name}`} className="text-blue-400 hover:text-blue-300">
              Browse
            </Link>
          </div>
          <h1 className="text-xl font-semibold mt-3">
            Settings —{" "}
            <span className="font-mono">{db.name}</span>
          </h1>
          <p className="text-sm text-neutral-400 mt-1">
            Owner: {db.owner}
            {db.description && ` · ${db.description}`}
          </p>
        </div>

        {/* Success/Error Messages */}
        {successMessage && (
          <div className="bg-green-900/30 border border-green-700 rounded-lg p-3 mb-4">
            <p className="text-sm text-green-300">{successMessage}</p>
          </div>
        )}
        {error && (
          <div className="bg-red-900/30 border border-red-700 rounded-lg p-3 mb-4">
            <p className="text-sm text-red-300">{error}</p>
          </div>
        )}

        {/* Status toggle section */}
        <div className="border border-neutral-800 rounded-lg p-5 mb-4">
          <h2 className="text-sm font-medium text-white mb-1">Database status</h2>
          <p className="text-xs text-neutral-400 mb-4">
            Inactive databases reject all service and internal-network requests.
            The admin dashboard (Browse, Settings) still works regardless.
          </p>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span
                className={`inline-block w-2 h-2 rounded-full shrink-0 ${
                  db.status === "active" ? "bg-green-400" : "bg-yellow-500"
                }`}
              />
              <span className="text-sm text-neutral-300">
                {db.status === "active" ? "Active" : "Inactive"}
              </span>
            </div>
            <button
              onClick={handleToggleStatus}
              disabled={statusLoading}
              className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none disabled:opacity-50 ${
                db.status === "active" ? "bg-green-600" : "bg-neutral-600"
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform duration-200 ${
                  db.status === "active" ? "translate-x-4" : "translate-x-0"
                }`}
              />
            </button>
          </div>
        </div>

        {/* Danger zone */}
        <div className="border border-red-900/60 rounded-lg p-5 mt-4">
          <h2 className="text-sm font-medium text-red-400 mb-1">Danger zone</h2>
          
          {/* Reset DB section */}
          <div className="mb-6 pb-6 border-b border-red-900/30">
            <h3 className="text-sm font-medium text-red-300 mb-1">Reset database</h3>
            <p className="text-xs text-neutral-400 mb-3">
              Remove all tables from this database. This action{" "}
              <span className="text-white font-medium">cannot be undone</span>, but the database file will remain.
            </p>
            {!confirmReset ? (
              <button
                onClick={() => setConfirmReset(true)}
                className="text-sm px-4 py-2 rounded border border-red-600 text-red-400 hover:border-red-500 hover:text-red-300 transition-colors"
              >
                Reset database
              </button>
            ) : (
              <div className="flex items-center gap-2">
                <span className="text-xs text-neutral-400">Are you sure? This will delete all tables.</span>
                <button
                  onClick={handleResetDatabase}
                  disabled={resetLoading}
                  className="text-sm px-3 py-1.5 rounded bg-red-900 text-red-200 hover:bg-red-800 transition-colors disabled:opacity-50"
                >
                  {resetLoading ? "Resetting…" : "Yes, reset"}
                </button>
                <button
                  onClick={() => setConfirmReset(false)}
                  className="text-sm px-3 py-1.5 rounded border border-neutral-700 text-neutral-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>

          {/* Drop DB section */}
          <div>
            <h3 className="text-sm font-medium text-red-300 mb-1">Delete database</h3>
            <p className="text-xs text-neutral-400 mb-3">
              Permanently drop this database and all its data.{" "}
              <span className="text-white font-medium">This cannot be undone</span>.
              The database file will be deleted from disk.
            </p>
            <p className="text-xs text-neutral-400 mb-2">
              Type{" "}
              <span className="font-mono text-white">{name}</span>{" "}
              to confirm:
            </p>
            <input
              type="text"
              value={confirmDropInput}
              onChange={(e) => setConfirmDropInput(e.target.value)}
              placeholder={name}
              className="w-full text-sm bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-white placeholder:text-neutral-600 outline-none focus:border-red-700 mb-3"
            />
            <button
              onClick={handleDropDatabase}
              disabled={confirmDropInput !== name || dropLoading}
              className="text-sm px-4 py-2 rounded bg-red-700 text-white hover:bg-red-600 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {dropLoading ? "Dropping…" : "Drop database"}
            </button>
          </div>
          
          {error && (
            <p className="text-xs text-red-400 mt-3">{error}</p>
          )}
        </div>
      </div>
    </div>
  );
}

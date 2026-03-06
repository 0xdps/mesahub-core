"use client";

import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";

interface DbInfo {
  name: string;
  owner: string;
  description: string | null;
  has_service_secret: boolean;
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
  const [actionLoading, setActionLoading] = useState(false);
  const [error, setError] = useState("");
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const [statusLoading, setStatusLoading] = useState(false);
  const [confirmDropInput, setConfirmDropInput] = useState("");
  const [dropLoading, setDropLoading] = useState(false);

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

  async function handleGenerate() {
    setActionLoading(true);
    setError("");
    setRevealedSecret(null);
    const res = await fetch(`/api/db/${name}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "generate_secret" }),
    });
    setActionLoading(false);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to generate secret");
      return;
    }
    const data = await res.json();
    setRevealedSecret(data.service_secret);
    setDb((prev) => prev ? { ...prev, has_service_secret: true } : prev);
  }

  async function handleRevoke() {
    setActionLoading(true);
    setError("");
    const res = await fetch(`/api/db/${name}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "revoke_secret" }),
    });
    setActionLoading(false);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setError(data.error ?? "Failed to revoke secret");
      return;
    }
    setDb((prev) => prev ? { ...prev, has_service_secret: false } : prev);
    setRevealedSecret(null);
    setConfirmRevoke(false);
  }

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

  function handleCopy() {
    if (!revealedSecret) return;
    navigator.clipboard.writeText(revealedSecret);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
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
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
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

        {/* Service secret section */}
        <div className={`border rounded-lg p-5 transition-opacity ${
          db.status === "active"
            ? "border-neutral-800"
            : "border-neutral-800 opacity-50 pointer-events-none"
        }`}>
          <h2 className="text-sm font-medium text-white mb-1">Service secret</h2>
          <p className="text-xs text-neutral-400 mb-4">
            A per-DB bearer token used by services to authenticate exec and
            query API calls. Secrets are only shown once — immediately after
            generation.
          </p>

          {/* Status indicator */}
          <div className="flex items-center gap-2 mb-5">
            <span
              className={`inline-block w-2 h-2 rounded-full shrink-0 ${
                db.has_service_secret ? "bg-green-400" : "bg-neutral-600"
              }`}
            />
            <span className="text-sm text-neutral-300">
              {db.has_service_secret
                ? "Active secret exists"
                : "No secret — access gated by internal network only"}
            </span>
          </div>

          {/* Revealed secret */}
          {revealedSecret && (
            <div className="bg-neutral-900 border border-neutral-700 rounded p-4 mb-5">
              <p className="text-xs text-neutral-500 mb-2 uppercase tracking-wide">
                New secret — copy now
              </p>
              <div className="flex items-center gap-3">
                <code className="text-sm text-green-400 break-all flex-1 select-all">
                  {revealedSecret}
                </code>
                <button
                  onClick={handleCopy}
                  className="shrink-0 text-xs border border-neutral-600 rounded px-3 py-1.5 hover:border-neutral-400 transition-colors text-neutral-300 hover:text-white"
                >
                  {copied ? "Copied!" : "Copy"}
                </button>
              </div>
              <p className="text-xs text-neutral-500 mt-2">
                Pass as{" "}
                <code className="text-neutral-300">
                  Authorization: Bearer &lt;secret&gt;
                </code>
              </p>
            </div>
          )}

          {error && <p className="text-xs text-red-400 mb-4">{error}</p>}

          {/* Action buttons */}
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleGenerate}
              disabled={actionLoading}
              className="text-sm px-3 py-1.5 rounded border border-neutral-600 text-neutral-200 hover:border-neutral-400 hover:text-white transition-colors disabled:opacity-50"
            >
              {actionLoading
                ? "…"
                : db.has_service_secret
                ? "Regenerate secret"
                : "Generate secret"}
            </button>

            {db.has_service_secret && !confirmRevoke && (
              <button
                onClick={() => setConfirmRevoke(true)}
                disabled={actionLoading}
                className="text-sm px-3 py-1.5 rounded border border-red-900 text-red-400 hover:border-red-600 hover:text-red-300 transition-colors disabled:opacity-50"
              >
                Revoke secret
              </button>
            )}

            {confirmRevoke && (
              <div className="flex items-center gap-2">
                <span className="text-xs text-neutral-400">Are you sure?</span>
                <button
                  onClick={handleRevoke}
                  disabled={actionLoading}
                  className="text-sm px-3 py-1.5 rounded bg-red-900 text-red-200 hover:bg-red-800 transition-colors disabled:opacity-50"
                >
                  Yes, revoke
                </button>
                <button
                  onClick={() => setConfirmRevoke(false)}
                  className="text-sm px-3 py-1.5 rounded border border-neutral-700 text-neutral-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
        </div>

        {/* Danger zone */}
        <div className="border border-red-900/60 rounded-lg p-5 mt-4">
          <h2 className="text-sm font-medium text-red-400 mb-1">Danger zone</h2>
          <p className="text-xs text-neutral-400 mb-4">
            Permanently drop this database and all its data. This action{" "}
            <span className="text-white font-medium">cannot be undone</span>.
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
          {error && (
            <p className="text-xs text-red-400 mt-2">{error}</p>
          )}
        </div>
      </div>
    </div>
  );
}

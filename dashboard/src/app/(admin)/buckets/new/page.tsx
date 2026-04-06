"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

interface UserOption {
  id: string;
  email: string;
}

interface QueryResponse {
  rows?: Record<string, unknown>[];
}

export default function NewBucketPage() {
  const router = useRouter();

  const [users, setUsers] = useState<UserOption[]>([]);
  const [loadingUsers, setLoadingUsers] = useState(true);

  const [userId, setUserId] = useState("");
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function loadUsers() {
      setLoadingUsers(true);

      const res = await fetch("/api/system/db/control/query", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          sql: "SELECT id, email FROM users ORDER BY created_at DESC LIMIT 500",
        }),
      }).catch(() => null);

      if (!res?.ok) {
        if (!cancelled) setLoadingUsers(false);
        return;
      }

      const data = (await res.json().catch(() => ({}))) as QueryResponse;
      const nextUsers = (data.rows ?? [])
        .map((row) => ({
          id: String(row.id ?? ""),
          email: String(row.email ?? ""),
        }))
        .filter((row) => row.id.length > 0);

      if (!cancelled) {
        setUsers(nextUsers);
        if (nextUsers.length > 0) {
          setUserId(nextUsers[0]!.id);
        }
        setLoadingUsers(false);
      }
    }

    loadUsers().catch(() => {
      if (!cancelled) setLoadingUsers(false);
    });

    return () => {
      cancelled = true;
    };
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const res = await fetch("/api/buckets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        userId,
        name,
        displayName: displayName || name,
        description: description || undefined,
      }),
    }).catch(() => null);

    setLoading(false);

    if (!res?.ok) {
      const data = await res?.json().catch(() => ({ error: "Unknown error" }));
      setError(data?.error ?? "Failed to create bucket");
      return;
    }

    router.push("/buckets");
    router.refresh();
  }

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
      <div className="max-w-lg">
        <div className="mb-6">
          <Link href="/buckets" className="text-sm text-neutral-400 hover:text-white">
            ← Back to buckets
          </Link>
          <h1 className="text-xl font-semibold mt-3">Create new bucket</h1>
          <p className="text-sm text-neutral-400 mt-1">
            Creates a bucket metadata record in <code className="text-neutral-300">control.db</code>.
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-neutral-300 mb-1">
              User
            </label>
            {loadingUsers ? (
              <div className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-neutral-400">
                Loading users...
              </div>
            ) : users.length > 0 ? (
              <select
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                required
                className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-neutral-500"
              >
                {users.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.email || user.id}
                  </option>
                ))}
              </select>
            ) : (
              <input
                type="text"
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                placeholder="user id"
                required
                className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-500"
              />
            )}
            <p className="text-xs text-neutral-500 mt-1">
              No users found? Enter a valid user ID manually.
            </p>
          </div>

          <div>
            <label className="block text-sm text-neutral-300 mb-1">
              Internal name <span className="text-neutral-500">(lowercase, hyphens, underscores)</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="media-assets"
              pattern="^[a-z0-9_-]+$"
              required
              className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-500"
            />
          </div>

          <div>
            <label className="block text-sm text-neutral-300 mb-1">
              Display name <span className="text-neutral-500">(optional)</span>
            </label>
            <input
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="Media assets"
              className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-500"
            />
          </div>

          <div>
            <label className="block text-sm text-neutral-300 mb-1">
              Description <span className="text-neutral-500">(optional)</span>
            </label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="File storage for static assets"
              className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-500"
            />
          </div>

          {error && (
            <p className="text-sm text-red-400 bg-red-950/50 border border-red-800 rounded px-3 py-2">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={loading || loadingUsers}
            className="w-full bg-white text-black text-sm font-medium py-2 rounded hover:bg-neutral-200 transition-colors disabled:opacity-50"
          >
            {loading ? "Creating..." : "Create bucket"}
          </button>
        </form>
      </div>
    </div>
  );
}

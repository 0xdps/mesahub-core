"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewBucketPage() {
  const router = useRouter();

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const res = await fetch("/api/buckets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
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
            ← Back to files
          </Link>
          <h1 className="text-xl font-semibold mt-3">Create new bucket</h1>
          <p className="text-sm text-neutral-400 mt-1">
            Creates a bucket record in <code className="text-neutral-300">store.db</code>.
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-neutral-300 mb-1">
              Name <span className="text-neutral-500">(lowercase, hyphens, underscores)</span>
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
            disabled={loading}
            className="w-full bg-white text-black text-sm font-medium py-2 rounded hover:bg-neutral-200 transition-colors disabled:opacity-50"
          >
            {loading ? "Creating..." : "Create bucket"}
          </button>
        </form>
      </div>
    </div>
  );
}


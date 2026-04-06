"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewDatabasePage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const res = await fetch("/api/db", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
        owner,
        description: description || undefined,
      }),
    });

    setLoading(false);

    if (!res.ok) {
      const data = await res.json().catch(() => ({ error: "Unknown error" }));
      setError(data.error ?? "Failed to create database");
      return;
    }

    router.push("/");
    router.refresh();
  }

  return (
    <div className="px-6 py-8 max-w-5xl mx-auto w-full">
    <div className="max-w-lg">
      <div className="mb-6">
        <Link href="/" className="text-sm text-neutral-400 hover:text-white">
          ← Back
        </Link>
        <h1 className="text-xl font-semibold mt-3">Register new database</h1>
        <p className="text-sm text-neutral-400 mt-1">
          Creates a new <code className="text-neutral-300">.db</code> file on the volume and registers it.
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
            placeholder="my-service"
            pattern="^[a-z0-9_-]+$"
            required
            className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-500"
          />
        </div>

        <div>
          <label className="block text-sm text-neutral-300 mb-1">Owner / service</label>
          <input
            type="text"
            value={owner}
            onChange={(e) => setOwner(e.target.value)}
            placeholder="team-backend"
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
            placeholder="Job queue state for billing service"
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
          {loading ? "Creating…" : "Create database"}
        </button>
      </form>
    </div>
    </div>
  );
}

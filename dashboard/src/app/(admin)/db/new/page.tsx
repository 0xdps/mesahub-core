"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewDatabasePage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [description, setDescription] = useState("");
  const [generateSecret, setGenerateSecret] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

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
        generate_secret: generateSecret,
      }),
    });

    setLoading(false);

    if (!res.ok) {
      const data = await res.json().catch(() => ({ error: "Unknown error" }));
      setError(data.error ?? "Failed to create database");
      return;
    }

    const data = await res.json();
    if (data.service_secret) {
      setCreatedSecret(data.service_secret);
    } else {
      router.push("/");
      router.refresh();
    }
  }

  function handleCopy() {
    if (!createdSecret) return;
    navigator.clipboard.writeText(createdSecret);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  // --- Secret reveal screen ---
  if (createdSecret) {
    return (
      <div className="px-6 py-8 max-w-5xl mx-auto w-full">
        <div className="max-w-lg">
          <div className="mb-6">
            <h1 className="text-xl font-semibold">Service secret generated</h1>
            <p className="text-sm text-neutral-400 mt-1">
              Copy this secret now — it will <span className="text-white font-medium">never be shown again</span>.
            </p>
          </div>

          <div className="bg-neutral-900 border border-neutral-700 rounded p-4 mb-4">
            <p className="text-xs text-neutral-500 mb-2 uppercase tracking-wide">Service secret</p>
            <div className="flex items-center gap-3">
              <code className="text-sm text-green-400 break-all flex-1 select-all">{createdSecret}</code>
              <button
                onClick={handleCopy}
                className="shrink-0 text-xs border border-neutral-600 rounded px-3 py-1.5 hover:border-neutral-400 transition-colors text-neutral-300 hover:text-white"
              >
                {copied ? "Copied!" : "Copy"}
              </button>
            </div>
          </div>

          <p className="text-xs text-neutral-500 mb-6">
            Pass this as <code className="text-neutral-300">Authorization: Bearer &lt;secret&gt;</code> from your service when calling the exec or query API.
          </p>

          <button
            onClick={() => { router.push("/"); router.refresh(); }}
            className="w-full bg-white text-black text-sm font-medium py-2 rounded hover:bg-neutral-200 transition-colors"
          >
            Done — go to dashboard
          </button>
        </div>
      </div>
    );
  }

  // --- Creation form ---
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

        <label className="flex items-start gap-3 cursor-pointer select-none group">
          <div className="relative mt-0.5">
            <input
              type="checkbox"
              checked={generateSecret}
              onChange={(e) => setGenerateSecret(e.target.checked)}
              className="sr-only"
            />
            <div className={`w-4 h-4 rounded border flex items-center justify-center transition-colors ${generateSecret ? "bg-white border-white" : "bg-neutral-900 border-neutral-600 group-hover:border-neutral-400"}`}>
              {generateSecret && (
                <svg className="w-2.5 h-2.5 text-black" viewBox="0 0 10 8" fill="none">
                  <path d="M1 4l3 3 5-6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
              )}
            </div>
          </div>
          <div>
            <p className="text-sm text-neutral-300">Generate service secret</p>
            <p className="text-xs text-neutral-500 mt-0.5">Creates a scoped token this service can use instead of the admin token. Shown once after creation.</p>
          </div>
        </label>

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

"use client";

import { useState } from "react";
import { DbViewer, SqlEditor, ResultsGrid } from "@sqlite-hub/ui";

// Mock query function that returns sample data
async function mockQueryFn(sql: string) {
  await new Promise((r) => setTimeout(r, 300));
  if (sql.toLowerCase().includes("select")) {
    return [
      { id: 1, name: "Alice", email: "alice@example.com", created_at: "2026-01-01" },
      { id: 2, name: "Bob", email: "bob@example.com", created_at: "2026-01-15" },
      { id: 3, name: "Carol", email: "carol@example.com", created_at: "2026-02-01" },
    ];
  }
  return [];
}

export default function UiTestPage() {
  const [sql, setSql] = useState("SELECT * FROM users;");
  const [rows, setRows] = useState<Record<string, unknown>[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function runQuery() {
    setLoading(true);
    setError(null);
    try {
      const result = await mockQueryFn(sql);
      setRows(result);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen bg-neutral-950 text-white p-8">
      <h1 className="text-2xl font-bold mb-2">@sqlite-hub/ui integration test</h1>
      <p className="text-neutral-400 text-sm mb-8">
        Testing published npm package components — v0.1.1
      </p>

      {/* Section 1: SqlEditor */}
      <section className="mb-8">
        <h2 className="text-sm font-semibold text-neutral-400 uppercase tracking-wide mb-3">
          SqlEditor
        </h2>
        <div className="rounded-lg border border-neutral-800 overflow-hidden">
          <SqlEditor value={sql} onChange={setSql} height="150px" />
        </div>
        <button
          onClick={runQuery}
          className="mt-3 px-4 py-2 bg-white text-black text-sm font-medium rounded hover:bg-neutral-200 transition-colors"
        >
          Run Query
        </button>
      </section>

      {/* Section 2: ResultsGrid */}
      <section className="mb-8">
        <h2 className="text-sm font-semibold text-neutral-400 uppercase tracking-wide mb-3">
          ResultsGrid
        </h2>
        <div className="rounded-lg border border-neutral-800 overflow-hidden bg-white">
          <ResultsGrid rows={rows} loading={loading} error={error} />
        </div>
      </section>

      {/* Section 3: DbViewer (full combined component) */}
      <section>
        <h2 className="text-sm font-semibold text-neutral-400 uppercase tracking-wide mb-3">
          DbViewer
        </h2>
        <div className="rounded-lg border border-neutral-800 overflow-hidden" style={{ height: "400px" }}>
          <DbViewer
            dbId="test-database"
            queryFn={mockQueryFn}
            tables={["users", "api_keys", "databases", "sessions"]}
          />
        </div>
      </section>
    </div>
  );
}

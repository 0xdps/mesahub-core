"use client";

import dynamic from "next/dynamic";
import { use } from "react";

const DbViewer = dynamic(
  () => import("@/app/(admin)/db/[name]/_viewer"),
  {
    ssr: false,
    loading: () => (
      <div className="flex-1 flex items-center justify-center">
        <span className="text-xs text-zinc-500">Loading editor…</span>
      </div>
    ),
  }
);

export default function SystemDbViewerPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name = "" } = use(params);

  return (
    <div className="h-full flex-1 relative flex flex-col">
      <div className="flex items-center gap-3 bg-neutral-900 border-b border-neutral-700 px-4 py-2 text-xs text-neutral-400 shrink-0">
        <svg
          width="12"
          height="12"
          viewBox="0 0 12 12"
          fill="none"
          className="shrink-0 text-amber-400"
          aria-hidden="true"
        >
          <path
            d="M6 1L6 7M6 9.5L6 10.5"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
          />
          <circle cx="6" cy="6" r="5" stroke="currentColor" strokeWidth="1" />
        </svg>
        <span>
          <span className="font-medium text-neutral-300">Protected — read-only.</span>{" "}
          This is an internal system database. Write statements are blocked.
        </span>
      </div>
      <div className="flex-1 min-h-0">
        <DbViewer name={name} system readOnly />
      </div>
    </div>
  );
}


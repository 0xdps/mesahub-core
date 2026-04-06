"use client";

import { Studio } from "@/components/gui/studio";
import { StudioExtensionManager } from "@/core/extension-manager";
import { createSQLiteExtensions } from "@/core/standard-extension";
import SystemDbDriver from "@/drivers/database/system-filedb";
import { use, useMemo } from "react";

export default function SystemDbViewerPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name = "" } = use(params);

  const driver = useMemo(() => new SystemDbDriver(name), [name]);

  const extensions = useMemo(
    () => new StudioExtensionManager(createSQLiteExtensions()),
    []
  );

  return (
    <div className="h-full flex-1 relative flex flex-col">
      {/* Read-only notice */}
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
        <Studio
          driver={driver}
          extensions={extensions}
          name={name}
          color="gray"
          readOnly
        />
      </div>
    </div>
  );
}

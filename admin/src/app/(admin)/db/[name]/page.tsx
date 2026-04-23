"use client";

import dynamic from "next/dynamic";
import { use, useEffect, useState } from "react";

const DbViewer = dynamic(() => import("./_viewer"), {
  ssr: false,
  loading: () => (
    <div className="flex-1 flex items-center justify-center">
      <span className="text-xs text-zinc-500">Loading editor…</span>
    </div>
  ),
});

export default function DbViewerPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name } = use(params);
  const [inactive, setInactive] = useState(false);

  useEffect(() => {
    fetch(`/api/db/${encodeURIComponent(name)}`)
      .then((r) => r.json())
      .then((data) => {
        if (data.status !== "active") setInactive(true);
      })
      .catch(() => {});
  }, [name]);

  return (
    <div className="h-full flex-1 relative flex flex-col">
      {inactive && (
        <div className="flex items-center gap-3 bg-yellow-950 border-b border-yellow-800 px-4 py-2 text-xs text-yellow-300 shrink-0">
          <span>
            <span className="font-medium">Read-only view</span>{" "}&mdash;{" "}
            this database is inactive. Re-activate it in{" "}
            <a href={`/db/${name}/settings`} className="underline hover:text-yellow-100">
              Settings
            </a>{" "}
            to allow writes.
          </span>
        </div>
      )}
      <div className="flex-1 min-h-0">
        <DbViewer name={name} />
      </div>
    </div>
  );
}

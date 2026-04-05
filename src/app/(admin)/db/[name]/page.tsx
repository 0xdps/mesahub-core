"use client";

import { Studio } from "@/components/gui/studio";
import { StudioExtensionManager } from "@/core/extension-manager";
import { createSQLiteExtensions } from "@/core/standard-extension";
import FilebDbDriver from "@/drivers/database/filedb";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";

export default function DbViewerPage() {
  const { name = '' } = useParams<{ name: string }>();
  const [inactive, setInactive] = useState(false);

  useEffect(() => {
    fetch(`/api/db/${encodeURIComponent(name)}`)
      .then((r) => r.json())
      .then((data) => { if (data.status !== "active") setInactive(true); })
      .catch(() => {});
  }, [name]);

  const driver = useMemo(() => new FilebDbDriver(name), [name]);

  const extensions = useMemo(
    () => new StudioExtensionManager(createSQLiteExtensions()),
    []
  );

  return (
    <div className="h-full flex-1 relative flex flex-col">
      {inactive && (
        <div className="flex items-center gap-3 bg-yellow-950 border-b border-yellow-800 px-4 py-2 text-xs text-yellow-300 shrink-0">
          <span>
            <span className="font-medium">Read-only view</span>{" "}&mdash;{" "}this database is inactive. Re-activate it in{" "}
            <a href={`/db/${name}/settings`} className="underline hover:text-yellow-100">
              Settings
            </a>{" "}
            to allow writes.
          </span>
        </div>
      )}
      <div className="flex-1 min-h-0">
        <Studio
          driver={driver}
          extensions={extensions}
          name={name}
          color="gray"
        />
      </div>
    </div>
  );
}

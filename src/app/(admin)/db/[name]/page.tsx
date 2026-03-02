"use client";

import { Studio } from "@/components/gui/studio";
import { StudioExtensionManager } from "@/core/extension-manager";
import { createSQLiteExtensions } from "@/core/standard-extension";
import FilebDbDriver from "@/drivers/database/filedb";
import { use, useMemo } from "react";

export default function DbViewerPage({
  params,
}: {
  params: Promise<{ name: string }>;
}) {
  const { name } = use(params);

  const driver = useMemo(() => new FilebDbDriver(name), [name]);

  const extensions = useMemo(
    () => new StudioExtensionManager(createSQLiteExtensions()),
    []
  );

  return (
    <div className="h-full flex-1 dark">
      <Studio
        driver={driver}
        extensions={extensions}
        name={name}
        color="gray"
      />
    </div>
  );
}

"use client";

import { useMemo } from "react";
import { Studio } from "@/components/gui/studio";
import { StudioExtensionManager } from "@/core/extension-manager";
import { createSQLiteExtensions } from "@/core/standard-extension";
import FilebDbDriver from "@/drivers/database/filedb";
import SystemDbDriver from "@/drivers/database/system-filedb";

interface Props {
  name: string;
  system?: boolean;
  readOnly?: boolean;
}

export default function DbViewer({ name, system = false, readOnly = false }: Props) {
  const driver = useMemo(
    () => system ? new SystemDbDriver(name) : new FilebDbDriver(name),
    [name, system]
  );
  const extensions = useMemo(
    () => new StudioExtensionManager(createSQLiteExtensions()),
    []
  );

  return (
    <Studio
      driver={driver}
      extensions={extensions}
      name={name}
      color="gray"
      {...(readOnly ? { readOnly: true } : {})}
    />
  );
}

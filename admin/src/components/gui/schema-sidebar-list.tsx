import { useCommonDialog } from "@/components/common-dialog";
import { useStudioContext } from "@/context/driver-provider";
import { useSchema } from "@/context/schema-provider";
import { OpenContextMenuList } from "@/core/channel-builtin";
import { scc } from "@/core/command";
import { DatabaseSchemaItem } from "@/drivers/base-driver";
import { triggerEditorExtensionTab } from "@/extensions/trigger-editor";
import { ExportFormat, exportTableData } from "@/lib/export-helper";
import { Icon, Table } from "@phosphor-icons/react";
import { LucideCog, LucideClipboardCopy, LucideDatabase, LucideDownload, LucideEraser, LucideFileCode2, LucideFileJson, LucideFileSpreadsheet, LucideFileText, LucidePencil, LucidePlus, LucideRefreshCw, LucideTable, LucideTrash, LucideUpload, LucideView } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { ListView, ListViewItem } from "../listview";
import { CloudflareIcon } from "../resource-card/icon";
import SchemaCreateDialog from "./schema-editor/schema-create";

interface SchemaListProps {
  search: string;
}

function formatTableSize(byteCount?: number) {
  const byteInKb = 1024;
  const byteInMb = byteInKb * 1024;
  const byteInGb = byteInMb * 1024;

  if (!byteCount) return undefined;
  if (byteInMb * 999 < byteCount)
    return (byteCount / byteInGb).toFixed(1) + " GB";
  if (byteInMb * 100 < byteCount)
    return (byteCount / byteInMb).toFixed(0) + " MB";
  if (byteInKb * 100 < byteCount)
    return (byteCount / byteInMb).toFixed(1) + " MB";
  if (byteInKb < byteCount) return Math.floor(byteCount / byteInKb) + " KB";
  return "1 KB";
}

function prepareListViewItem(
  schema: DatabaseSchemaItem[],
  maxTableSize: number
): ListViewItem<DatabaseSchemaItem>[] {
  return schema.map((s) => {
    let icon = Table;
    let iconClassName = "";

    console.log("ss", s);

    if (s.type === "trigger") {
      icon = LucideCog;
      iconClassName = "text-purple-500";
    } else if (s.type === "view") {
      icon = LucideView;
      iconClassName = "text-green-600 dark:text-green-300";
    } else if (s.type === "table" && s.name === "_cf_KV") {
      icon = CloudflareIcon as Icon;
      iconClassName = "text-orange-500";
    }

    return {
      data: s,
      icon: icon,
      iconColor: iconClassName,
      key: s.schemaName + "." + s.name,
      name: s.name,
      progressBarMax: maxTableSize,
      progressBarValue: s.tableSchema?.stats?.sizeInByte,
      progressBarLabel: formatTableSize(s.tableSchema?.stats?.sizeInByte),
    };
  });
}

function groupTriggerByTable(
  items: ListViewItem<DatabaseSchemaItem>[]
): ListViewItem<DatabaseSchemaItem>[] {
  // Find all triggers
  const triggers = items.filter((item) => item.data.type === "trigger");
  const triggerByTable = triggers.reduce(
    (a, b) => {
      a[b.data.tableName ?? ""] = [...(a[b.data.tableName ?? ""] ?? []), b];
      return a;
    },
    {} as Record<string, ListViewItem<DatabaseSchemaItem>[]>
  );

  const list = items.filter((item) => item.data.type !== "trigger");
  for (const item of list) {
    if (item.data.type === "table" && triggerByTable[item.data.name]) {
      item.children = [
        ...(item.children ?? []),
        ...(triggerByTable[item.name] ?? []),
      ];
    }
  }

  return list;
}

function groupByFtsTable(items: ListViewItem<DatabaseSchemaItem>[]) {
  const hash = items.reduce(
    (a, b) => {
      a[b.name] = b;
      return a;
    },
    {} as Record<string, ListViewItem<DatabaseSchemaItem>>
  );
  const ftsSuffix = ["_config", "_content", "_data", "_docsize", "_idx"];
  const excludes = new Set();

  for (const item of items) {
    if (item.data.tableSchema?.fts5) {
      item.children = ftsSuffix
        .map((suffix) => hash[item.data.name + suffix])
        .filter(Boolean);

      ftsSuffix.forEach((suffix) => excludes.add(item.data.name + suffix));

      item.badgeContent = "fts5";
    }
  }

  return items.filter((item) => !excludes.has(item.data.name));
}

function sortTable(items: ListViewItem<DatabaseSchemaItem>[]) {
  return items.sort((a, b) => a.name.localeCompare(b.name));
}

function flattenSchemaGroup(
  schemaGroup: ListViewItem<DatabaseSchemaItem>[]
): ListViewItem<DatabaseSchemaItem>[] {
  if (schemaGroup.length === 1) return schemaGroup[0].children ?? [];
  return schemaGroup;
}

// Copy of export-result-button.tsx
async function downloadExportTable(
  format: string,
  handler: Promise<string | Blob>,
  dbName: string,
  tableName: string
) {
  try {
    if (!format) return;
    const content = await handler;
    if (!content) return;
    // TODO: more mimeTypes support
    const blob =
      content instanceof Blob
        ? content
        : new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${dbName}-${tableName}.${format === "delimited" ? "csv" : format}`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (error) {
    console.error(`Failed to download exported ${format} file:`, error);
  }
}

export default function SchemaList({ search }: Readonly<SchemaListProps>) {
  const { databaseDriver, extensions, name: dbName } = useStudioContext();
  const [selected, setSelected] = useState("");
  const { refresh, schema, currentSchemaName } = useSchema();
  const { showDialog } = useCommonDialog();
  const [editSchema, setEditSchema] = useState<string | null>(null);

  const [collapsed, setCollapsed] = useState(() => {
    return new Set<string>();
  });

  useEffect(() => {
    setSelected("");
  }, [setSelected, search]);

  const exportFormats = useMemo(() => {
    return [
      { title: "Export as CSV",        format: "csv",  icon: LucideFileText },
      { title: "Export as Excel",      format: "xlsx", icon: LucideFileSpreadsheet },
      { title: "Export as JSON",       format: "json", icon: LucideFileJson },
      { title: "Export as SQL INSERT", format: "sql",  icon: LucideFileCode2 },
    ];
  }, []);

  const handleImportFile = useCallback(
    async (file: File, schemaName: string, tableName: string) => {
      try {
        const text = await file.text();
        const fileExt = file.name.split(".").pop()?.toLowerCase();
        
        let rows: Record<string, unknown>[] = [];
        
        if (fileExt === "json") {
          const parsed = JSON.parse(text);
          rows = Array.isArray(parsed) ? parsed : [parsed];
        } else if (fileExt === "csv") {
          // Simple CSV parser (handles quoted values)
          const lines = text.split("\n").filter(line => line.trim());
          if (lines.length < 2) {
            toast.error("CSV file must have at least a header row and one data row");
            return;
          }
          
          const parseCSVLine = (line: string): string[] => {
            const result: string[] = [];
            let current = "";
            let inQuotes = false;
            
            for (let i = 0; i < line.length; i++) {
              const char = line[i];
              if (char === '"') {
                if (inQuotes && line[i + 1] === '"') {
                  current += '"';
                  i++;
                } else {
                  inQuotes = !inQuotes;
                }
              } else if (char === "," && !inQuotes) {
                result.push(current.trim());
                current = "";
              } else {
                current += char;
              }
            }
            result.push(current.trim());
            return result;
          };
          
          const headers = parseCSVLine(lines[0]);
          
          for (let i = 1; i < lines.length; i++) {
            const values = parseCSVLine(lines[i]);
            const row: Record<string, unknown> = {};
            headers.forEach((header, index) => {
              const value = values[index] || null;
              // Convert "NULL" string to actual null
              row[header] = value === "NULL" || value === "" ? null : value;
            });
            rows.push(row);
          }
        } else {
          toast.error("Unsupported file format. Please use CSV or JSON.");
          return;
        }

        if (rows.length === 0) {
          toast.error("No data found in file");
          return;
        }

        // Get table columns
        const tableInfo = await databaseDriver.query(
          `SELECT * FROM ${databaseDriver.escapeId(tableName)} LIMIT 0`
        );
        
        const columns = tableInfo.headers.map(h => h.name);
        
        // Insert data in batches
        const batchSize = 100;
        let inserted = 0;
        
        for (let i = 0; i < rows.length; i += batchSize) {
          const batch = rows.slice(i, i + batchSize);
          
          for (const row of batch) {
            const cols = Object.keys(row).filter(k => columns.includes(k));
            if (cols.length === 0) continue;
            
            const colNames = cols.map(c => databaseDriver.escapeId(c)).join(", ");
            const values = cols.map(c => databaseDriver.escapeValue(row[c])).join(", ");
            
            await databaseDriver.query(
              `INSERT INTO ${databaseDriver.escapeId(tableName)} (${colNames}) VALUES (${values})`
            );
            inserted++;
          }
        }
        
        toast.success(`Imported ${inserted} row${inserted !== 1 ? "s" : ""} into ${tableName}`);
        refresh();
      } catch (error) {
        console.error("Import error:", error);
        toast.error(`Import failed: ${(error as Error).message}`);
      }
    },
    [databaseDriver, refresh]
  );

  const prepareContextMenu = useCallback(
    (item?: DatabaseSchemaItem) => {
      const selectedName = item?.name;
      const isTable = item?.type === "table";
      const schemaName = item?.schemaName ?? currentSchemaName;

      const createMenuSection = {
        title: "Create",
        icon: LucidePlus,
        sub: [
          databaseDriver.getFlags().supportCreateUpdateTable && {
            title: "Create Table",
            icon: LucideTable,
            onClick: () => {
              scc.tabs.openBuiltinSchema({
                schemaName: item?.schemaName ?? currentSchemaName,
              });
            },
          },
          ...extensions.getResourceCreateMenu(),
        ].filter(Boolean),
      };

      const modificationSection = item
        ? [
            isTable && databaseDriver.getFlags().supportCreateUpdateTable
              ? {
                  title: "Edit Table",
                  icon: LucidePencil,
                  onClick: () => {
                    scc.tabs.openBuiltinSchema({
                      schemaName: item?.schemaName ?? currentSchemaName,
                      tableName: item?.name,
                    });
                  },
                }
              : undefined,
            ...extensions.getResourceContextMenu(item, "modification"),
          ].filter(Boolean)
        : [];

      const exportSection =
        isTable && selectedName
          ? {
              title: "Export Table",
              icon: LucideDownload,
              sub: exportFormats.map(({ title, format, icon }) => ({
                title,
                icon,
                onClick: async () => {
                  const handler = exportTableData(
                    databaseDriver,
                    schemaName,
                    selectedName,
                    format as ExportFormat,
                    "file"
                  );
                  downloadExportTable(format, handler, dbName, selectedName);
                },
              })),
            }
          : undefined;

      const importSection =
        isTable && selectedName
          ? {
              title: "Import Data",
              icon: LucideUpload,
              onClick: () => {
                // Trigger file input
                const input = document.createElement("input");
                input.type = "file";
                input.accept = ".csv,.json";
                input.onchange = async (e) => {
                  const file = (e.target as HTMLInputElement).files?.[0];
                  if (file) {
                    await handleImportFile(file, schemaName, selectedName);
                  }
                };
                input.click();
              },
            }
          : undefined;

      return [
        createMenuSection,
        {
          title: "Copy Name",
          icon: LucideClipboardCopy,
          disabled: !selectedName,
          onClick: () => {
            window.navigator.clipboard.writeText(selectedName ?? "");
          },
        },
        { separator: true },

        // Export Section
        exportSection,
        // Import Section
        importSection,
        // Modification Section
        ...modificationSection,
        modificationSection.length > 0 ? { separator: true } : undefined,

        // Destructive section
        isTable && selectedName
          ? { separator: true }
          : undefined,
        isTable && selectedName
          ? {
              title: "Truncate Table",
              destructive: true,
              icon: LucideEraser,
              onClick: () => {
                showDialog({
                  title: "Truncate Table",
                  content: `All rows in "${selectedName}" will be permanently deleted. This cannot be undone.`,
                  destructive: true,
                  actions: [
                    {
                      text: "Truncate",
                      onClick: async () => {
                        await databaseDriver.query(
                          `DELETE FROM ${databaseDriver.escapeId(selectedName)}`
                        );
                      },
                      onComplete: () => refresh(),
                    },
                  ],
                });
              },
            }
          : undefined,
        isTable && selectedName
          ? {
              title: "Drop Table",
              destructive: true,
              icon: LucideTrash,
              onClick: () => {
                showDialog({
                  title: "Drop Table",
                  content: `Table "${selectedName}" and all its data will be permanently dropped. This cannot be undone.`,
                  destructive: true,
                  actions: [
                    {
                      text: "Drop",
                      onClick: async () => {
                        await databaseDriver.query(
                          `DROP TABLE ${databaseDriver.escapeId(selectedName)}`
                        );
                      },
                      onComplete: () => refresh(),
                    },
                  ],
                });
              },
            }
          : undefined,

        { title: "Refresh", icon: LucideRefreshCw, onClick: () => refresh() },
      ].filter(Boolean) as OpenContextMenuList;
    },
    [refresh, databaseDriver, currentSchemaName, extensions, exportFormats, showDialog, handleImportFile, dbName]
  );

  const listViewItems = useMemo(() => {
    const r = sortTable(
      Object.entries(schema).map(([s, tables]) => {
        const maxTableSize = Math.max(
          ...tables.map((t) => t.tableSchema?.stats?.sizeInByte ?? 0)
        );

        return {
          data: { type: "schema", schemaName: s },
          icon: LucideDatabase,
          name: s,
          iconBadgeColor: s === currentSchemaName ? "bg-green-600" : undefined,
          key: s.toString(),
          children: sortTable(
            groupByFtsTable(
              groupTriggerByTable(prepareListViewItem(tables, maxTableSize))
            )
          ),
        } as ListViewItem<DatabaseSchemaItem>;
      })
    );

    if (databaseDriver.getFlags().optionalSchema) {
      // For SQLite, the default schema is main and
      // it is optional.
      return flattenSchemaGroup(r);
    }
    return r;
  }, [schema, currentSchemaName, databaseDriver]);

  const filterCallback = useCallback(
    (item: ListViewItem<DatabaseSchemaItem>) => {
      if (!search) return true;
      return item.name.toLowerCase().indexOf(search.toLowerCase()) >= 0;
    },
    [search]
  );

  return (
    <>
      {editSchema && (
        <SchemaCreateDialog
          schemaName={editSchema}
          onClose={() => setEditSchema(null)}
        />
      )}
      <ListView
        full
        filter={filterCallback}
        highlight={search}
        items={listViewItems}
        collapsedKeys={collapsed}
        onCollapsedChange={setCollapsed}
        onContextMenu={(item) => prepareContextMenu(item?.data)}
        selectedKey={selected}
        onSelectChange={setSelected}
        onDoubleClick={(item) => {
          if (item.data.type === "table" || item.data.type === "view") {
            scc.tabs.openBuiltinTable({
              schemaName: item.data.schemaName ?? "",
              tableName: item.data.name,
            });
          } else if (item.data.type === "trigger") {
            triggerEditorExtensionTab.open({
              schemaName: item.data.schemaName ?? "",
              name: item.name ?? "",
              tableName: item.data.tableName ?? "",
            });
          } else if (item.data.type === "schema") {
            if (databaseDriver.getFlags().supportUseStatement) {
              const dialect = databaseDriver.getFlags().dialect;
              const switch_keyword =
                dialect === "postgres" ? "SET search_path TO " : "USE ";
              const name = [databaseDriver.escapeId(item.name)];
              if (dialect === "postgres") {
                name.push(databaseDriver.escapeId("$user"));
                if (item.name !== "public") {
                  name.push(databaseDriver.escapeId("public"));
                }
              }
              databaseDriver.query(switch_keyword + name.join(",")).then(() => {
                refresh();
              });
            }
          }
        }}
      />
    </>
  );
}

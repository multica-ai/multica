import type { ToolPolicyRow } from "../core";

export type PermissionType =
  | "Multica"
  | "Runtime"
  | "Connections"
  | "Repos"
  | "Credentials";

function isRuntimeReportedSource(row: ToolPolicyRow): boolean {
  return row.source === "runtime_report" || row.source === "scan";
}

export function permissionType(row: ToolPolicyRow): PermissionType {
  if (row.source === "connection") return "Connections";
  if (row.source === "credential") return "Credentials";
  if (row.resource_pattern) return "Repos";
  if (isRuntimeReportedSource(row) || row.category === "Runtimes") {
    return "Runtime";
  }
  return "Multica";
}

export interface PresentedCapabilityRow {
  /** Stable presentation key. Composite rows do not pretend to be backend keys. */
  key: string;
  /** Human label shown in the catalog and Role editor. */
  title: string;
  /** One or more canonical rows that this single presentation controls. */
  rows: ToolPolicyRow[];
}

const FILE_WRITE_KEYS = new Set(["create_file", "edit_file"]);

export function isFileWriteToolKey(toolKey: string): boolean {
  return FILE_WRITE_KEYS.has(toolKey.replace(/^tools:/i, "").toLowerCase());
}

// Creating and editing are one user intent. They remain separate canonical
// enforcement keys, but the authoring UI presents and changes them together.
export function presentCapabilityRows(
  rows: ToolPolicyRow[],
): PresentedCapabilityRow[] {
  const fileWriteRows = rows.filter((row) => isFileWriteToolKey(row.tool_key));
  const shouldCombine = fileWriteRows.length >= 2;
  const presented: PresentedCapabilityRow[] = [];
  let insertedFileWrite = false;

  for (const row of rows) {
    if (shouldCombine && isFileWriteToolKey(row.tool_key)) {
      if (!insertedFileWrite) {
        presented.push({
          key: "file-write",
          title: "Create and edit files",
          rows: fileWriteRows,
        });
        insertedFileWrite = true;
      }
      continue;
    }
    presented.push({
      key: row.resource_pattern
        ? `${row.tool_key}:${row.resource_pattern}`
        : row.tool_key,
      title: row.title || row.tool_key,
      rows: [row],
    });
  }
  return presented;
}

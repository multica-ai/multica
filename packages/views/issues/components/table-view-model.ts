import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type {
  TableCalculation,
  TableColumnKey,
} from "@multica/core/issues/stores/view-store";
import type {
  Issue,
  IssuePropertyValue,
} from "@multica/core/types";

/** Export must fail closed when paged Table responses cannot prove that the
 * complete query window was collected. The UI translates this marker instead
 * of exposing a protocol detail to users. */
export class IssueTableExportIntegrityError extends Error {
  constructor() {
    super("Table export response was incomplete");
    this.name = "IssueTableExportIntegrityError";
  }
}

export type IssueTableDisplayRow =
  | {
      kind: "group";
      key: string;
      label: string;
      count: number;
      collapsed: boolean;
    }
  | {
      kind: "issue";
      key: string;
      issue: Issue;
      depth: number;
      hasChildren: boolean;
      collapsed: boolean;
    }
  // Stands in for a row that has not arrived yet. Cold loads render these in
  // the table's own grid rather than swapping the surface for a generic
  // placeholder: columns, widths and the header are known before any data is,
  // so only the rows need standing in for, and the layout does not shift when
  // they are replaced.
  | { kind: "skeleton"; key: string }
  // Carries the state rather than a finished label: the row renders through
  // ListLoadMoreFooter, the same footer Board / List / Swimlane use, which owns
  // the wording and the styling for each state.
  | {
      kind: "load_more";
      key: string;
      state: "loading" | "has_more" | "error" | "end";
      // Rows past this many mean the branch actually paginated, which is what
      // decides whether reaching the end is worth marking.
      total: number;
      onLoad?: () => void;
    };

export function getIssueTableSelectionRange(
  issueIds: string[],
  anchorId: string | null,
  targetId: string,
): string[] | null {
  if (!anchorId) return null;
  const anchorIndex = issueIds.indexOf(anchorId);
  const targetIndex = issueIds.indexOf(targetId);
  if (anchorIndex === -1 || targetIndex === -1) return null;

  const start = Math.min(anchorIndex, targetIndex);
  const end = Math.max(anchorIndex, targetIndex);
  return issueIds.slice(start, end + 1);
}

/**
 * Refresh the issue objects inside a frozen row snapshot. While a cell editor
 * popup is open the table renders a structural snapshot (row order, grouping,
 * nesting stay put so the popup's anchor row cannot be reordered out of the
 * virtualized render window mid-interaction), but the VALUES inside those
 * rows must stay live — a multi-select toggle, for example, commits while the
 * popup is open and its checkmark has to reflect the optimistic cache.
 * Issues deleted from the live window keep their stale snapshot object; the
 * structure catches up the moment the editor closes.
 */
export function refreshFrozenTableRows(
  snapshot: IssueTableDisplayRow[],
  issueById: ReadonlyMap<string, Issue>,
): IssueTableDisplayRow[] {
  return snapshot.map((row) => {
    if (row.kind !== "issue") return row;
    const live = issueById.get(row.issue.id);
    return live && live !== row.issue ? { ...row, issue: live } : row;
  });
}
function columnValue(
  issue: Issue,
  columnKey: TableColumnKey,
): IssuePropertyValue | string | number | null | undefined {
  const propertyId = propertyIdFromViewKey(columnKey);
  if (propertyId) return issue.properties[propertyId];
  switch (columnKey) {
    case "identifier":
      return issue.identifier;
    case "title":
      return issue.title;
    case "status":
      return issue.status;
    case "priority":
      return issue.priority;
    case "assignee":
      return issue.assignee_id;
    case "labels":
      return issue.labels?.map((label) => label.name).join(", ");
    case "project":
      return issue.project_id;
    case "start_date":
      return issue.start_date;
    case "due_date":
      return issue.due_date;
    case "created_at":
      return issue.created_at;
    case "updated_at":
      return issue.updated_at;
    case "child_progress":
      return undefined;
    case "creator":
      return issue.creator_id;
  }
  return undefined;
}

export function calculateIssueTableColumn(
  issues: Issue[],
  columnKey: TableColumnKey,
  calculation: TableCalculation,
) {
  if (calculation === "none") return null;
  const values = issues
    .map((issue) => columnValue(issue, columnKey))
    .filter((value) => value !== undefined && value !== null && value !== "");
  if (calculation === "count") return values.length;
  const numbers = values.filter((value): value is number => typeof value === "number");
  if (numbers.length === 0) return null;
  const sum = numbers.reduce((total, value) => total + value, 0);
  return calculation === "sum" ? sum : sum / numbers.length;
}

function escapeCsvCell(value: unknown) {
  const raw = value == null ? "" : String(value);
  // Spreadsheet apps execute string cells beginning with these characters as
  // formulas. Prefix text (including headers) with an apostrophe so exported
  // user-controlled titles/properties remain data. Preserve real numbers as
  // numbers — a negative numeric value is not an injected formula string.
  const text =
    typeof value === "string" && /^[=+\-@\t\r]/.test(raw)
      ? `'${raw}`
      : raw;
  return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

export function buildIssueTableCsv(
  headers: string[],
  rows: unknown[][],
) {
  return [headers, ...rows]
    .map((row) => row.map(escapeCsvCell).join(","))
    .join("\r\n");
}

import { ALL_STATUSES } from "@multica/core/issues/config";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type {
  TableCalculation,
  TableColumnKey,
  TableGrouping,
} from "@multica/core/issues/stores/view-store";
import type {
  Issue,
  IssueProperty,
  IssuePropertyValue,
} from "@multica/core/types";

/**
 * Ceiling for the whole-window structure features (grouping, hierarchy).
 * Both are only truthful over a COMPLETE window, and completing the window
 * means materializing every offset page — at 100 rows/page an unbounded
 * materialize would issue total/100 sequential GETs and pin every entity in
 * memory (grouping is persisted view state, so it would re-run on every
 * visit). Below the ceiling the table auto-loads the ~10 remaining pages;
 * above it, structure features suspend with an explicit notice instead of
 * triggering an unbounded download.
 */
export const TABLE_STRUCTURE_MAX_WINDOW = 1000;

/** True when the window is too large for grouping/hierarchy — see
 *  TABLE_STRUCTURE_MAX_WINDOW. `total` is the server-reported window size
 *  (0 while unknown, which must NOT suspend: an unknown window is handled
 *  by the windowComplete gate, not this ceiling). */
export function isTableStructureSuspended(total: number): boolean {
  return total > TABLE_STRUCTURE_MAX_WINDOW;
}

/**
 * Single decision point for EVERY auto-pagination loop over a table window —
 * the structure materialization loop and the working (ids-facet) window
 * loop. The two share the main table's query cache when the agents-working
 * filter is on, so a loop that skipped these gates would re-open the very
 * ceiling the other just enforced (round-5 review P1). Every gate exists
 * because its absence was a concrete failure mode:
 *
 * - `hasError`: a page that keeps failing leaves `hasNextPage` true and
 *   `isFetchingNextPage` false after each attempt, so an ungated effect
 *   refires forever — a silent request storm
 *   (round-4 review P1#1). Terminal errors stop the loop; resuming is an
 *   explicit user Retry.
 * - `loadedCount`: an ABSOLUTE stop at the ceiling, independent of any
 *   server-reported total. The suspension check alone is not a hard limit
 *   when totals drift between pages — a stale small page-1 total kept the
 *   ceiling open while pagination advanced toward a much larger real window
 *   (round-4 review P1#2).
 * - `total` must be the FRESHEST page's total (pagination itself advances on
 *   the latest page), for the same reason.
 */
export function shouldAutoLoadNextWindowPage(input: {
  windowWanted: boolean;
  total: number;
  loadedCount: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  hasError: boolean;
}): boolean {
  if (!input.windowWanted || input.hasError) return false;
  if (isTableStructureSuspended(input.total)) return false;
  if (input.loadedCount >= TABLE_STRUCTURE_MAX_WINDOW) return false;
  return input.hasNextPage && !input.isFetchingNextPage;
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
    };

export interface BuildIssueTableRowsOptions {
  grouping: TableGrouping;
  properties: IssueProperty[];
  collapsedGroups: ReadonlySet<string>;
  collapsedParents: ReadonlySet<string>;
  hierarchy: boolean;
  /**
   * Whether `issues` is the FULL window (no unfetched pages). Hierarchy
   * nesting and parent-based group assignment only apply to a complete
   * window: deriving structure from loaded pages re-parents rows as later
   * pages arrive — a child renders as root, then jumps under its parent /
   * into another group mid-scroll (round-2 review P2#3). While incomplete,
   * rows stay in flat sort order and group by their own fields, which is
   * stable under pagination; the tree assembles once, when the data is all
   * there.
   */
  windowComplete: boolean;
  getActorName: (type: string, id: string) => string;
  getStatusLabel: (status: Issue["status"]) => string;
  noValueLabel: string;
  unassignedLabel: string;
  trueLabel: string;
  falseLabel: string;
}

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

function propertyValueLabel(
  property: IssueProperty | undefined,
  value: IssuePropertyValue | undefined,
  labels: Pick<
    BuildIssueTableRowsOptions,
    "noValueLabel" | "trueLabel" | "falseLabel"
  >,
) {
  if (!property || value === undefined) return labels.noValueLabel;
  const propertyOptions = property.config.options ?? [];
  if (property.type === "select") {
    return (
      propertyOptions.find((option) => option.id === value)?.name ??
      labels.noValueLabel
    );
  }
  if (property.type === "multi_select") {
    const ids = Array.isArray(value) ? value : [];
    const names = propertyOptions
      .filter((option) => ids.includes(option.id))
      .map((option) => option.name);
    return names.length > 0 ? names.join(", ") : labels.noValueLabel;
  }
  if (property.type === "checkbox") {
    return value === true ? labels.trueLabel : labels.falseLabel;
  }
  return String(value);
}

function groupDescriptor(
  issue: Issue,
  options: BuildIssueTableRowsOptions,
) {
  if (options.grouping === "status") {
    return {
      key: `status:${issue.status}`,
      label: options.getStatusLabel(issue.status),
    };
  }
  if (options.grouping === "assignee") {
    if (!issue.assignee_type || !issue.assignee_id) {
      return { key: "assignee:none", label: options.unassignedLabel };
    }
    return {
      key: `assignee:${issue.assignee_type}:${issue.assignee_id}`,
      label: options.getActorName(issue.assignee_type, issue.assignee_id),
    };
  }
  const propertyId = propertyIdFromViewKey(options.grouping);
  if (propertyId) {
    const property = options.properties.find((item) => item.id === propertyId);
    const value = issue.properties[propertyId];
    const label = propertyValueLabel(
      property,
      value,
      options,
    );
    const valueKey = Array.isArray(value)
      ? [...value].sort().join(",")
      : value === undefined
        ? "none"
        : String(value);
    return { key: `property:${propertyId}:${valueKey}`, label };
  }
  return { key: "none", label: "" };
}

function hierarchyRows(
  issues: Issue[],
  collapsedParents: ReadonlySet<string>,
  hierarchy: boolean,
) {
  if (!hierarchy) {
    return issues.map<IssueTableDisplayRow>((issue) => ({
      kind: "issue",
      key: issue.id,
      issue,
      depth: 0,
      hasChildren: false,
      collapsed: false,
    }));
  }

  const issueIds = new Set(issues.map((issue) => issue.id));
  const children = new Map<string, Issue[]>();
  for (const issue of issues) {
    if (!issue.parent_issue_id || !issueIds.has(issue.parent_issue_id)) continue;
    const siblings = children.get(issue.parent_issue_id) ?? [];
    siblings.push(issue);
    children.set(issue.parent_issue_id, siblings);
  }
  const roots = issues.filter(
    (issue) => !issue.parent_issue_id || !issueIds.has(issue.parent_issue_id),
  );
  const rows: IssueTableDisplayRow[] = [];
  const visited = new Set<string>();

  const markHiddenDescendantsVisited = (issue: Issue) => {
    for (const child of children.get(issue.id) ?? []) {
      if (visited.has(child.id)) continue;
      visited.add(child.id);
      markHiddenDescendantsVisited(child);
    }
  };

  const visit = (issue: Issue, depth: number) => {
    if (visited.has(issue.id)) return;
    visited.add(issue.id);
    const issueChildren = children.get(issue.id) ?? [];
    const collapsed = collapsedParents.has(issue.id);
    rows.push({
      kind: "issue",
      key: issue.id,
      issue,
      depth,
      hasChildren: issueChildren.length > 0,
      collapsed,
    });
    if (collapsed) {
      // The final orphan/cycle recovery pass below must not resurrect rows
      // intentionally hidden under a collapsed parent.
      markHiddenDescendantsVisited(issue);
      return;
    }
    for (const child of issueChildren) visit(child, depth + 1);
  };

  for (const root of roots) visit(root, 0);
  // Cycles and cross-page orphans degrade to top-level rows instead of
  // disappearing from the table.
  for (const issue of issues) visit(issue, 0);
  return rows;
}

export function buildIssueTableRows(
  issues: Issue[],
  options: BuildIssueTableRowsOptions,
): IssueTableDisplayRow[] {
  // See BuildIssueTableRowsOptions.windowComplete — parent-derived structure
  // is only trustworthy (and stable) once every page is loaded.
  const applyHierarchy = options.hierarchy && options.windowComplete;
  if (options.grouping === "none") {
    return hierarchyRows(
      issues,
      options.collapsedParents,
      applyHierarchy,
    );
  }

  const issueById = new Map(issues.map((issue) => [issue.id, issue]));
  const groupSource = (issue: Issue) => {
    if (!applyHierarchy) return issue;
    let current = issue;
    const seen = new Set<string>();
    while (current.parent_issue_id && !seen.has(current.id)) {
      seen.add(current.id);
      const parent = issueById.get(current.parent_issue_id);
      if (!parent) break;
      current = parent;
    }
    return current;
  };

  const groups = new Map<string, { label: string; issues: Issue[] }>();
  for (const issue of issues) {
    const descriptor = groupDescriptor(groupSource(issue), options);
    const group = groups.get(descriptor.key) ?? {
      label: descriptor.label,
      issues: [],
    };
    group.issues.push(issue);
    groups.set(descriptor.key, group);
  }

  const entries = [...groups.entries()];
  if (options.grouping === "status") {
    const rank = new Map(
      ALL_STATUSES.map((status, index) => [`status:${status}`, index]),
    );
    entries.sort(
      ([a], [b]) => (rank.get(a) ?? Number.MAX_SAFE_INTEGER) - (rank.get(b) ?? Number.MAX_SAFE_INTEGER),
    );
  }

  const rows: IssueTableDisplayRow[] = [];
  for (const [key, group] of entries) {
    const collapsed = options.collapsedGroups.has(key);
    rows.push({
      kind: "group",
      key,
      label: group.label,
      count: group.issues.length,
      collapsed,
    });
    if (!collapsed) {
      rows.push(
        ...hierarchyRows(
          group.issues,
          options.collapsedParents,
          applyHierarchy,
        ),
      );
    }
  }
  return rows;
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

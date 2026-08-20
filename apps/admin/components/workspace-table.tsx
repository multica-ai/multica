"use client";

import { ArrowDown, ArrowUp, ArrowUpDown, Inbox } from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { StatusBadge } from "./status-badge";
import { formatCost } from "@/lib/format";
import { SORTABLE_COLUMNS, type SortColumn, type SortDirection, type WorkspaceListItem } from "@/lib/types";

const COLUMNS: Array<{ key: SortColumn; label: string }> = [
  { key: "status", label: "Status" },
  { key: "name", label: "Workspace" },
  { key: "owner", label: "Owner" },
  { key: "model", label: "Model" },
  { key: "llmKey", label: "LLM key" },
  { key: "team", label: "Team" },
  { key: "keySpend", label: "Cost" },
  { key: "issues", label: "Open issues" },
  { key: "activity", label: "Last activity" },
];

function formatRelativeTime(iso: string | null): string {
  if (!iso) return "Never";
  const diffMs = Date.parse(iso) - Date.now();
  const diffMinutes = Math.round(diffMs / 60_000);
  const abs = Math.abs(diffMinutes);
  if (abs < 1) return "Just now";
  if (abs < 60) return `${abs}m ago`;
  const diffHours = Math.round(abs / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.round(diffHours / 24);
  return `${diffDays}d ago`;
}

interface WorkspaceTableProps {
  items: WorkspaceListItem[];
  sort: SortColumn;
  direction: SortDirection;
  onSortChange: (column: SortColumn) => void;
  onRowClick: (item: WorkspaceListItem) => void;
  selectedId?: string;
  /** Whether a search/status filter is currently narrowing the list — picks
   * between the two empty-state copies in plan §5.3 ("No workspaces found"
   * vs. "No workspaces match your filters"). */
  hasActiveFilters?: boolean;
  onClearFilters?: () => void;
}

export function WorkspaceTable({
  items,
  sort,
  direction,
  onSortChange,
  onRowClick,
  selectedId,
  hasActiveFilters = false,
  onClearFilters,
}: WorkspaceTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {COLUMNS.map((col) => {
            // llmKey/team aren't sortable at the SQL layer (see SORTABLE_COLUMNS'
            // doc comment) — render them as plain headers rather than a sort
            // control that would show an "active" arrow while the rows are
            // actually still ordered by whatever the last real sort was.
            if (!SORTABLE_COLUMNS.includes(col.key)) {
              return (
                <TableHead key={col.key} className="text-muted-foreground">
                  {col.label}
                </TableHead>
              );
            }
            const active = sort === col.key;
            const Icon = active ? (direction === "asc" ? ArrowUp : ArrowDown) : ArrowUpDown;
            return (
              <TableHead key={col.key}>
                <button
                  type="button"
                  onClick={() => onSortChange(col.key)}
                  className={cn(
                    "inline-flex items-center gap-1 hover:text-foreground",
                    active ? "text-foreground" : "text-muted-foreground",
                  )}
                >
                  {col.label}
                  <Icon className="size-3.5" aria-hidden />
                </button>
              </TableHead>
            );
          })}
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.length === 0 ? (
          <TableRow>
            <TableCell colSpan={COLUMNS.length} className="py-10">
              {/* Plan §5.3: two distinct empty states — an unfiltered empty
                  table (illustration + CTA) vs. a filtered-to-nothing table
                  (message + clear-filters link). No CTA target exists yet
                  for "create a workspace" from this admin surface, so that
                  state omits the action rather than linking somewhere
                  speculative. */}
              {hasActiveFilters ? (
                <Empty>
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <Inbox />
                    </EmptyMedia>
                    <EmptyTitle>No workspaces match your filters</EmptyTitle>
                    <EmptyDescription>
                      {onClearFilters ? (
                        <button type="button" onClick={onClearFilters} className="text-primary hover:underline">
                          Clear filters
                        </button>
                      ) : (
                        "Try adjusting your search or status filter."
                      )}
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              ) : (
                <Empty>
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <Inbox />
                    </EmptyMedia>
                    <EmptyTitle>No workspaces found</EmptyTitle>
                    <EmptyDescription>Workspaces will appear here once created.</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
            </TableCell>
          </TableRow>
        ) : (
          items.map((item) => (
            <TableRow
              key={item.id}
              onClick={() => onRowClick(item)}
              data-state={item.id === selectedId ? "selected" : undefined}
              className="cursor-pointer"
            >
              <TableCell>
                <StatusBadge status={item.status} />
              </TableCell>
              <TableCell className="font-medium text-foreground">{item.name}</TableCell>
              <TableCell>{item.owner ?? <span className="text-muted-foreground">Unassigned</span>}</TableCell>
              <TableCell>{item.model ?? <span className="text-muted-foreground">Not set</span>}</TableCell>
              <TableCell>{item.llmKey ?? <span className="text-muted-foreground">Not linked</span>}</TableCell>
              <TableCell>{item.team ?? <span className="text-muted-foreground">—</span>}</TableCell>
              <TableCell>{formatCost(item.keySpend)}</TableCell>
              <TableCell>{item.openIssues}</TableCell>
              <TableCell>{formatRelativeTime(item.lastActivity)}</TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}

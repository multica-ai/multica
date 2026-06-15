"use client";

import { useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  BarChart3,
  Bug,
  Clock,
  Code,
  FileSearch,
  GitPullRequest,
  Newspaper,
  Pause,
  Plus,
  Shield,
  Webhook,
  Zap,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import {
  useAutopilotsViewStore,
  AUTOPILOT_DEFAULT_HIDDEN_COLUMNS,
  AUTOPILOT_SCOPES,
  type AutopilotColumnKey,
  type AutopilotScope,
  type AutopilotSortField,
} from "@multica/core/autopilots/stores";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Autopilot } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  LIST_GRID_BOTTOM_CLEARANCE,
  ListGrid,
  ListGridBody,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
  type ListGridSortDirection,
} from "@multica/ui/components/ui/list-grid";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { AutopilotDialog } from "./autopilot-dialog";
import { AutopilotListToolbar, actorFilterValue } from "./autopilot-list-toolbar";
import {
  AutopilotBatchToolbar,
  AutopilotRowActions,
} from "./autopilot-list-actions";
import type { TriggerFrequency } from "./trigger-config";
import { useT, useTimeAgo } from "../../i18n";

// Column template — single source of truth for header, rows, and skeletons.
// Same conventions as the skills list (see list-grid.tsx and the comment
// there): deterministic var-width tracks (rows are virtualized), and
// TWO-ZONE responsiveness:
// - Container ≥ @2xl (672px): WYSIWYG — every user-enabled column renders;
//   the grid carries min-width = Σ(enabled tracks + gaps) and the wrapper
//   scrolls horizontally when the enabled set outgrows the container. An
//   enabled column must NEVER silently vanish (the "dead toggle" bug).
// - Container < @2xl: static core set (name + assignee), no horizontal
//   scroll, column toggles don't apply.
const GRID_COLS =
  "grid-cols-[0.75rem_1rem_minmax(120px,1fr)_var(--apc-assignee)_1.75rem_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_1rem_minmax(200px,1fr)_var(--apc-assignee)_var(--apc-trigger)_var(--apc-lastrun)_var(--apc-nextrun)_var(--apc-mode)_var(--apc-creator)_var(--apc-created)_1.75rem_0.75rem]";

// h-12 rows; the virtualizer's fixed-size contract.
const ROW_HEIGHT = 48;

// Single source for hideable column widths: track vars and the grid's
// min-width derive from the same numbers.
const COLUMN_WIDTHS: Record<AutopilotColumnKey, number> = {
  assignee: 144,
  trigger: 144,
  lastRun: 120,
  nextRun: 104,
  mode: 104,
  creator: 144,
  created: 104,
};

// Fixed tracks (edges 12+12, checkbox 16, name min 200, kebab 28) plus the
// 11 gap-x-3 gaps between the wide template's 12 tracks (zero-width tracks
// still carry gaps).
const FIXED_TRACKS_WIDTH = 268 + 11 * 12;

function columnTrackVars(
  isVisible: (key: AutopilotColumnKey) => boolean,
): React.CSSProperties {
  const width = (key: AutopilotColumnKey) =>
    isVisible(key) ? `${COLUMN_WIDTHS[key]}px` : "0px";
  const minWidth =
    FIXED_TRACKS_WIDTH +
    (Object.keys(COLUMN_WIDTHS) as AutopilotColumnKey[]).reduce(
      (sum, key) => sum + (isVisible(key) ? COLUMN_WIDTHS[key] : 0),
      0,
    );
  return {
    "--apc-assignee": width("assignee"),
    "--apc-trigger": width("trigger"),
    "--apc-lastrun": width("lastRun"),
    "--apc-nextrun": width("nextRun"),
    "--apc-mode": width("mode"),
    "--apc-creator": width("creator"),
    "--apc-created": width("created"),
    "--apc-minw": `${minWidth}px`,
  } as React.CSSProperties;
}

// ---------------------------------------------------------------------------
// Templates for the empty state (unchanged from the previous page version).
// Prompts stay raw English because they're injected directly into the
// agent's task input.
// ---------------------------------------------------------------------------

type TemplateId =
  | "daily_news"
  | "pr_review"
  | "bug_triage"
  | "weekly_progress"
  | "dependency_audit"
  | "documentation_check";

interface AutopilotTemplate {
  id: TemplateId;
  prompt: string;
  icon: typeof Zap;
  frequency: TriggerFrequency;
  time: string;
}

const TEMPLATES: AutopilotTemplate[] = [
  {
    id: "daily_news",
    prompt: `1. Search the web for news and announcements published today only (strictly today's date)
2. Filter for topics relevant to our team and industry
3. For each item, write a short summary including: title, source, key takeaways
4. Compile everything into a single digest post
5. Post the digest as a comment on this issue and @mention all workspace members`,
    icon: Newspaper,
    frequency: "daily",
    time: "09:00",
  },
  {
    id: "pr_review",
    prompt: `1. List all open pull requests in the repository
2. Identify PRs that have been open for more than 24 hours without a review
3. For each stale PR, note the author, age, and a one-line summary of the change
4. Post a comment on this issue listing all stale PRs with links
5. @mention the team to remind them to review`,
    icon: GitPullRequest,
    frequency: "weekdays",
    time: "10:00",
  },
  {
    id: "bug_triage",
    prompt: `1. List all issues with status "triage" or "backlog" that have not been prioritized
2. For each issue, read the description and any attached logs or screenshots
3. Assess severity (critical / high / medium / low) based on user impact and scope
4. Set the priority field on the issue accordingly
5. Add a comment explaining your assessment and suggested next steps`,
    icon: Bug,
    frequency: "weekdays",
    time: "09:00",
  },
  {
    id: "weekly_progress",
    prompt: `1. Gather all issues completed (status "done") in the past 7 days
2. Gather all issues currently in progress
3. Identify any blocked issues and their blockers
4. Calculate key metrics: issues closed, issues opened, net change
5. Write a structured weekly report with sections: Completed, In Progress, Blocked, Metrics
6. Post the report as a comment on this issue`,
    icon: BarChart3,
    frequency: "weekly",
    time: "17:00",
  },
  {
    id: "dependency_audit",
    prompt: `1. Run dependency audit tools on the project (npm audit, go vuln check, etc.)
2. Identify any packages with known security vulnerabilities
3. List outdated packages that are more than 2 major versions behind
4. For each finding, note the severity, affected package, and recommended fix
5. Post a summary report as a comment with actionable items`,
    icon: Shield,
    frequency: "weekly",
    time: "08:00",
  },
  {
    id: "documentation_check",
    prompt: `1. List all code changes merged in the past 7 days (via git log)
2. For each significant change, check if related documentation was updated
3. Identify any new APIs, config options, or features missing documentation
4. Create a list of documentation gaps with file paths and suggested content
5. Post the findings as a comment on this issue`,
    icon: FileSearch,
    frequency: "weekly",
    time: "14:00",
  },
];

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

function CheckboxCell({
  checked,
  onToggle,
}: {
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <ListGridCell className="justify-center px-0">
      <button
        type="button"
        aria-pressed={checked}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onToggle();
        }}
        className={`-m-1.5 flex items-center p-1.5 ${
          checked ? "" : "opacity-0 transition-opacity group-hover/row:opacity-100"
        }`}
      >
        <Checkbox
          checked={checked}
          tabIndex={-1}
          className="pointer-events-none"
        />
      </button>
    </ListGridCell>
  );
}

function NameCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  return (
    <ListGridCell className="gap-1.5">
      <span className="min-w-0 truncate text-sm font-medium">
        {autopilot.title}
      </span>
      {/* Paused marker: in the "all" scope active and paused rows mix, so a
          paused automation needs an inline signal. */}
      {autopilot.status === "paused" && (
        <span
          title={t(($) => $.status.paused)}
          className="flex shrink-0 items-center text-amber-500"
        >
          <Pause className="size-3" />
        </span>
      )}
    </ListGridCell>
  );
}

function AssigneeCell({ autopilot }: { autopilot: Autopilot }) {
  const { getActorName } = useActorName();
  return (
    <ListGridCell className="gap-1.5">
      <ActorAvatar
        actorType={autopilot.assignee_type}
        actorId={autopilot.assignee_id}
        size={18}
        enableHoverCard={autopilot.assignee_type === "agent"}
        showStatusDot={autopilot.assignee_type === "agent"}
      />
      <span className="min-w-0 truncate text-xs text-muted-foreground">
        {getActorName(autopilot.assignee_type, autopilot.assignee_id)}
      </span>
    </ListGridCell>
  );
}

const TRIGGER_ICONS: Record<string, typeof Zap> = {
  schedule: Clock,
  webhook: Webhook,
  api: Code,
};

function TriggerCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const kinds = autopilot.trigger_kinds ?? [];
  if (kinds.length === 0) {
    return (
      <ListGridCell className="hidden @2xl:flex">
        <span className="text-xs text-muted-foreground/40">—</span>
      </ListGridCell>
    );
  }
  return (
    <ListGridCell className="hidden gap-2 @2xl:flex">
      {kinds.map((kind) => {
        // Server-driven enum: unknown kinds get a generic icon + raw label.
        const Icon = TRIGGER_ICONS[kind] ?? Zap;
        const label =
          kind === "schedule" || kind === "webhook" || kind === "api"
            ? t(($) => $.trigger_kind[kind])
            : kind;
        return (
          <span
            key={kind}
            className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
          >
            <Icon className="size-3 shrink-0" />
            <span className="truncate">{label}</span>
          </span>
        );
      })}
    </ListGridCell>
  );
}

// Dot color per last run outcome. Server-driven enum — unknown values fall
// through to the neutral dot, never crash (API compatibility rule).
function runStatusDotClass(status: string | null | undefined): string {
  switch (status) {
    case "completed":
    case "issue_created":
      return "bg-emerald-500";
    case "failed":
      return "bg-red-500";
    case "skipped":
      return "bg-amber-500";
    case "running":
      return "bg-blue-500";
    default:
      return "bg-muted-foreground/40";
  }
}

function LastRunCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const timeAgo = useTimeAgo();
  if (!autopilot.last_run_at) {
    return (
      <ListGridCell className="hidden @2xl:flex">
        <span className="text-xs text-muted-foreground/40">—</span>
      </ListGridCell>
    );
  }
  const status = autopilot.last_run_status;
  const knownStatus =
    status === "issue_created" ||
    status === "running" ||
    status === "completed" ||
    status === "failed" ||
    status === "skipped"
      ? status
      : null;
  return (
    <ListGridCell className="hidden gap-1.5 @2xl:flex">
      <span
        title={knownStatus ? t(($) => $.run_status[knownStatus]) : status ?? undefined}
        className={`size-1.5 shrink-0 rounded-full ${runStatusDotClass(status)}`}
      />
      <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
        {timeAgo(autopilot.last_run_at)}
      </span>
    </ListGridCell>
  );
}

function NextRunCell({ autopilot }: { autopilot: Autopilot }) {
  const next = autopilot.next_run_at;
  return (
    <ListGridCell className="hidden @2xl:flex">
      {next ? (
        <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
          {new Date(next).toLocaleString(undefined, {
            month: "short",
            day: "numeric",
            hour: "2-digit",
            minute: "2-digit",
          })}
        </span>
      ) : (
        <span className="text-xs text-muted-foreground/40">—</span>
      )}
    </ListGridCell>
  );
}

function ModeCell({ autopilot }: { autopilot: Autopilot }) {
  const { t } = useT("autopilots");
  const mode = autopilot.execution_mode;
  const label =
    mode === "create_issue" || mode === "run_only"
      ? t(($) => $.execution_mode[mode])
      : mode;
  return (
    <ListGridCell className="hidden @2xl:flex">
      <span className="truncate text-xs text-muted-foreground">{label}</span>
    </ListGridCell>
  );
}

function CreatorCell({ autopilot }: { autopilot: Autopilot }) {
  const { getActorName } = useActorName();
  return (
    <ListGridCell className="hidden gap-1.5 @2xl:flex">
      <ActorAvatar
        actorType={autopilot.created_by_type}
        actorId={autopilot.created_by_id}
        size={18}
      />
      <span className="min-w-0 truncate text-xs text-muted-foreground">
        {getActorName(autopilot.created_by_type, autopilot.created_by_id)}
      </span>
    </ListGridCell>
  );
}

// ---------------------------------------------------------------------------
// Header row
// ---------------------------------------------------------------------------

function AutopilotListHeader({
  sortField,
  sortDirection,
  onSort,
  allSelected,
  someSelected,
  onToggleAll,
  isColVisible,
}: {
  sortField: AutopilotSortField;
  sortDirection: ListGridSortDirection;
  onSort: (field: AutopilotSortField) => void;
  allSelected: boolean;
  someSelected: boolean;
  onToggleAll: () => void;
  isColVisible: (key: AutopilotColumnKey) => boolean;
}) {
  const { t } = useT("autopilots");
  const sorted = (field: AutopilotSortField) =>
    sortField === field ? sortDirection : false;
  const anySelected = allSelected || someSelected;
  return (
    <ListGridHeader>
      <div className="flex items-center justify-center">
        <button
          type="button"
          aria-pressed={allSelected}
          onClick={onToggleAll}
          className={`-m-1.5 flex items-center p-1.5 ${
            anySelected
              ? ""
              : "opacity-0 transition-opacity group-hover/header:opacity-100"
          }`}
        >
          <Checkbox
            checked={allSelected}
            indeterminate={someSelected && !allSelected}
            tabIndex={-1}
            className="pointer-events-none"
          />
        </button>
      </div>
      <ListGridHeaderCell sorted={sorted("name")} onSort={() => onSort("name")}>
        {t(($) => $.page.table.name)}
      </ListGridHeaderCell>
      {isColVisible("assignee") ? (
        <ListGridHeaderCell>
          {t(($) => $.page.table.assignee)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="px-0" />
      )}
      {isColVisible("trigger") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.page.table.trigger)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("lastRun") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("lastRun")}
          onSort={() => onSort("lastRun")}
        >
          {t(($) => $.page.table.last_run)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("nextRun") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("nextRun")}
          onSort={() => onSort("nextRun")}
        >
          {t(($) => $.page.table.next_run)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("mode") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.page.table.mode)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("creator") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.page.table.created_by)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("created") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("created")}
          onSort={() => onSort("created")}
        >
          {t(($) => $.page.table.created)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      <span aria-hidden="true" />
    </ListGridHeader>
  );
}

function LoadingSkeleton() {
  return (
    <ListGrid
      className={GRID_COLS}
      style={columnTrackVars(
        (key) => !AUTOPILOT_DEFAULT_HIDDEN_COLUMNS.includes(key),
      )}
    >
      <ListGridHeader>
        <span aria-hidden="true" />
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-14" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        {/* mode/creator/created are hidden by default — keep their tracks
            mapped with empty placeholders. */}
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <span aria-hidden="true" />
      </ListGridHeader>
      {Array.from({ length: 5 }).map((_, i) => (
        <ListGridRow key={i} className="hover:bg-transparent">
          <span aria-hidden="true" />
          <ListGridCell>
            <Skeleton className="h-3.5 w-40 max-w-full" />
          </ListGridCell>
          <ListGridCell className="gap-1.5">
            <Skeleton className="size-5 rounded-full" />
            <Skeleton className="h-3 w-12" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-16" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-14" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-14" />
          </ListGridCell>
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <span aria-hidden="true" />
        </ListGridRow>
      ))}
    </ListGrid>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function AutopilotsPage() {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const {
    data: autopilots = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(autopilotListOptions(wsId));

  const [createOpen, setCreateOpen] = useState(false);
  const [selectedTemplate, setSelectedTemplate] =
    useState<AutopilotTemplate | null>(null);
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  // Persisted scope may hold a retired value (e.g. "archived" from an older
  // build) — fall back to "all" instead of stranding the user on an
  // unreachable scope.
  const rawScope = useAutopilotsViewStore((s) => s.scope);
  const scope = AUTOPILOT_SCOPES.includes(rawScope) ? rawScope : "all";
  const setScope = useAutopilotsViewStore((s) => s.setScope);
  const sortField = useAutopilotsViewStore((s) => s.sortField);
  const sortDirection = useAutopilotsViewStore((s) => s.sortDirection);
  const hiddenColumns = useAutopilotsViewStore((s) => s.hiddenColumns);
  const filters = useAutopilotsViewStore((s) => s.filters);
  const handleSort = useAutopilotsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useAutopilotsViewStore((s) => s.setSortField);
  const setSortDirection = useAutopilotsViewStore((s) => s.setSortDirection);
  const toggleColumn = useAutopilotsViewStore((s) => s.toggleColumn);
  const toggleFilter = useAutopilotsViewStore((s) => s.toggleFilter);
  const clearFilters = useAutopilotsViewStore((s) => s.clearFilters);

  const isColVisible = (key: AutopilotColumnKey) =>
    !hiddenColumns.includes(key);

  const toggleSelected = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // Scope counts come from the FULL set (filters never affect them — they
  // are stage inventories, not result counts). API-archived rows (no UI
  // flow creates them) are excluded everywhere.
  const scopeCounts = useMemo<Record<AutopilotScope, number>>(() => {
    let active = 0;
    let paused = 0;
    for (const a of autopilots) {
      if (a.status === "archived") continue;
      if (a.status === "paused") paused++;
      else active++;
    }
    return { all: active + paused, active, paused };
  }, [autopilots]);

  // Rows within the current scope, unfiltered — toolbar option lists and
  // the "n / total" denominator derive from this.
  const scopeRows = useMemo<Autopilot[]>(() => {
    if (scope === "all") {
      return autopilots.filter((a) => a.status !== "archived");
    }
    return autopilots.filter((a) => a.status === scope);
  }, [autopilots, scope]);

  // Visible rows: filters, then sort.
  const rows = useMemo<Autopilot[]>(() => {
    const filtered = scopeRows.filter((a) => {
      if (
        filters.assignees.length > 0 &&
        !filters.assignees.includes(
          actorFilterValue(a.assignee_type, a.assignee_id),
        )
      ) {
        return false;
      }
      if (filters.modes.length > 0 && !filters.modes.includes(a.execution_mode)) {
        return false;
      }
      if (
        filters.triggerKinds.length > 0 &&
        !(a.trigger_kinds ?? []).some((k) => filters.triggerKinds.includes(k))
      ) {
        return false;
      }
      if (
        filters.creators.length > 0 &&
        !filters.creators.includes(
          actorFilterValue(a.created_by_type, a.created_by_id),
        )
      ) {
        return false;
      }
      return true;
    });

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.title.localeCompare(b.title) * dir;
      }
      if (sortField === "nextRun") {
        // Missing next run sorts last regardless of direction.
        const av = a.next_run_at ? Date.parse(a.next_run_at) : null;
        const bv = b.next_run_at ? Date.parse(b.next_run_at) : null;
        if (av === null && bv === null) return a.title.localeCompare(b.title);
        if (av === null) return 1;
        if (bv === null) return -1;
        return (av - bv) * dir;
      }
      if (sortField === "created") {
        return (Date.parse(a.created_at) - Date.parse(b.created_at)) * dir;
      }
      // lastRun: never-ran rows sort as oldest.
      const av = a.last_run_at ? Date.parse(a.last_run_at) : 0;
      const bv = b.last_run_at ? Date.parse(b.last_run_at) : 0;
      return (av - bv) * dir || a.title.localeCompare(b.title);
    });
    return filtered;
  }, [scopeRows, filters, sortField, sortDirection]);

  // Row virtualization — same wiring as the skills list: headless math,
  // offsets as padding on the body, fixed-height rows.
  const listScrollRef = useRef<HTMLDivElement | null>(null);
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => listScrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
  });

  const openCreate = (template?: AutopilotTemplate) => {
    setSelectedTemplate(template ?? null);
    setCreateOpen(true);
  };

  const selectedRows = rows.filter((a) => selectedIds.has(a.id));
  const allSelected = rows.length > 0 && selectedRows.length === rows.length;
  const someSelected = selectedRows.length > 0 && !allSelected;
  const handleToggleAll = () => {
    setSelectedIds(allSelected ? new Set() : new Set(rows.map((a) => a.id)));
  };

  const virtualItems = rowVirtualizer.getVirtualItems();
  const firstVirtual = virtualItems[0];
  const lastVirtual = virtualItems[virtualItems.length - 1];
  const virtualPadding = {
    top: firstVirtual ? firstVirtual.start : 0,
    bottom: lastVirtual
      ? rowVirtualizer.getTotalSize() - lastVirtual.end
      : 0,
  };

  const totalCount = autopilots.length;
  const showEmpty = !isLoading && !listError && totalCount === 0;

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex flex-1 min-h-0 flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {totalCount > 0 && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {totalCount}
            </span>
          )}
        </div>
        {/* Quiet chrome button (outline, icon-only below md) — primary is
            reserved for the empty state's CTAs. */}
        <Button
          size="sm"
          variant="outline"
          className="h-8 w-8 gap-1 px-0 md:w-auto md:px-2.5"
          aria-label={t(($) => $.page.new_autopilot)}
          onClick={() => openCreate()}
        >
          <Plus className="h-3.5 w-3.5" />
          <span className="hidden md:inline">
            {t(($) => $.page.new_autopilot)}
          </span>
        </Button>
      </PageHeader>

      {listError ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <p className="text-sm text-muted-foreground">
            {listError instanceof Error ? listError.message : String(listError)}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetchList()}
          >
            {t(($) => $.page.retry)}
          </Button>
        </div>
      ) : isLoading ? (
        <div className="flex-1 overflow-y-auto @container">
          <LoadingSkeleton />
        </div>
      ) : showEmpty ? (
        <div className="flex flex-col items-center px-5 py-16">
          <Zap className="mb-3 h-10 w-10 text-muted-foreground opacity-30" />
          <p className="text-sm text-muted-foreground">
            {t(($) => $.page.empty.title)}
          </p>
          <p className="mb-6 mt-1 text-xs text-muted-foreground">
            {t(($) => $.page.empty.hint)}
          </p>
          <div className="grid w-full max-w-3xl grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {TEMPLATES.map((tpl) => {
              const Icon = tpl.icon;
              return (
                <button
                  key={tpl.id}
                  type="button"
                  className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-accent/40"
                  onClick={() => openCreate(tpl)}
                >
                  <Icon className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <div className="text-sm font-medium">
                      {t(($) => $.templates[tpl.id].title)}
                    </div>
                    <div className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">
                      {t(($) => $.templates[tpl.id].summary)}
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
          <Button
            size="sm"
            variant="outline"
            className="mt-4"
            onClick={() => openCreate()}
          >
            <Plus className="mr-1 h-3.5 w-3.5" />
            {t(($) => $.page.start_blank)}
          </Button>
        </div>
      ) : (
        <>
          <AutopilotListToolbar
            scope={scope}
            onScopeChange={setScope}
            scopeCounts={scopeCounts}
            filters={filters}
            onToggleFilter={toggleFilter}
            onClearFilters={clearFilters}
            sortField={sortField}
            sortDirection={sortDirection}
            onSortFieldChange={handleSortFieldSelect}
            onSortDirectionChange={setSortDirection}
            hiddenColumns={hiddenColumns}
            onToggleColumn={toggleColumn}
            allRows={scopeRows}
            visibleCount={rows.length}
          />
          <div
            ref={listScrollRef}
            className="min-h-0 flex-1 overflow-auto @container"
          >
            <ListGrid
              className={`${GRID_COLS} @2xl:min-w-[var(--apc-minw)]`}
              style={columnTrackVars(isColVisible)}
            >
              <AutopilotListHeader
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={handleSort}
                allSelected={allSelected}
                someSelected={someSelected}
                onToggleAll={handleToggleAll}
                isColVisible={isColVisible}
              />
              <ListGridBody
                style={{
                  paddingTop: virtualPadding.top,
                  paddingBottom:
                    virtualPadding.bottom + LIST_GRID_BOTTOM_CLEARANCE,
                }}
              >
                {rows.length === 0 && (
                  <div className="col-span-full py-16 text-center text-sm text-muted-foreground">
                    {t(($) => $.page.no_matches)}
                  </div>
                )}
                {virtualItems.map((vi) => {
                  const autopilot = rows[vi.index];
                  if (!autopilot) return null;
                  return (
                    <ListGridRow
                      key={autopilot.id}
                      className={
                        selectedIds.has(autopilot.id)
                          ? "bg-accent/30"
                          : undefined
                      }
                      render={
                        <AppLink href={wsPaths.autopilotDetail(autopilot.id)} />
                      }
                    >
                      <CheckboxCell
                        checked={selectedIds.has(autopilot.id)}
                        onToggle={() => toggleSelected(autopilot.id)}
                      />
                      <NameCell autopilot={autopilot} />
                      {isColVisible("assignee") ? (
                        <AssigneeCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="px-0" />
                      )}
                      {isColVisible("trigger") ? (
                        <TriggerCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("lastRun") ? (
                        <LastRunCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("nextRun") ? (
                        <NextRunCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("mode") ? (
                        <ModeCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("creator") ? (
                        <CreatorCell autopilot={autopilot} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("created") ? (
                        <ListGridCell className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground @2xl:flex">
                          {new Date(autopilot.created_at).toLocaleDateString()}
                        </ListGridCell>
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      <ListGridCell className="justify-end px-0">
                        <AutopilotRowActions row={autopilot} />
                      </ListGridCell>
                    </ListGridRow>
                  );
                })}
              </ListGridBody>
            </ListGrid>
          </div>
        </>
      )}

      <AutopilotBatchToolbar
        rows={selectedRows}
        onClear={() => setSelectedIds(new Set())}
      />

      {createOpen && (
        <AutopilotDialog
          mode="create"
          open={createOpen}
          onOpenChange={setCreateOpen}
          initial={
            selectedTemplate
              ? {
                  // Template title pulls from i18n so the user-visible default
                  // matches their locale, while the prompt body stays raw EN
                  // since it's injected directly into the agent task.
                  title: t(($) => $.templates[selectedTemplate.id].title),
                  description: selectedTemplate.prompt,
                }
              : undefined
          }
          initialTriggerConfig={
            selectedTemplate
              ? {
                  frequency: selectedTemplate.frequency,
                  time: selectedTemplate.time,
                }
              : undefined
          }
        />
      )}
    </div>
  );
}

"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  Archive,
  ArchiveRestore,
  Bot,
  Loader2,
  Lock,
  Plus,
  X,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  CreateAgentRequest,
  MemberWithUser,
} from "@multica/core/types";
import {
  type AgentActivity,
  agentRunCounts30dOptions,
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
  VISIBILITY_TOOLTIP,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import {
  useAgentsViewStore,
  AGENT_DEFAULT_HIDDEN_COLUMNS,
  AGENT_SCOPES,
  type AgentColumnKey,
  type AgentsScope,
  type AgentSortField,
} from "@multica/core/agents/stores";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useNavigation, useRowLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { availabilityConfig } from "../presence";
import { CreateAgentDialog } from "./create-agent-dialog";
import { AgentRowActions } from "./agent-row-actions";
import { AgentListToolbar } from "./agent-list-toolbar";
import { useT } from "../../i18n";

// Column template — single source of truth for header, rows, and skeletons.
// Same conventions as the skills/autopilots lists (see list-grid.tsx):
// deterministic var-width tracks, two-zone responsiveness (≥@2xl WYSIWYG
// with min-width + horizontal-scroll escape valve; <@2xl static core set of
// name + status, toggles don't apply).
//
// Agents are identity-type entities (few, avatar + persona), so rows are
// the TWO-LINE form: avatar left, name + description right, 64px tall —
// the documented exception to the single-line management-list rule.
const GRID_COLS =
  "grid-cols-[0.75rem_minmax(120px,1fr)_var(--agc-status-mobile)_1.75rem_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_1rem_minmax(200px,1fr)_var(--agc-status-desktop)_var(--agc-owner)_var(--agc-runtime)_var(--agc-lastactive)_var(--agc-runs)_var(--agc-model)_var(--agc-created)_1.75rem_0.75rem]";

// Two-line rows; the virtualizer's fixed-size contract.
const ROW_HEIGHT = 64;

// Single source for hideable column widths: track vars and the grid's
// min-width derive from the same numbers.
const COLUMN_WIDTHS: Record<AgentColumnKey, number> = {
  // Sized for the worst case "Online · 2 tasks" (~140px incl. padding);
  // idle rows show only the dot + label and leave some in-track slack.
  status: 144,
  owner: 144,
  runtime: 144,
  lastActive: 120,
  runs: 88,
  model: 120,
  created: 104,
};

// Fixed tracks (edges 12+12, checkbox 16, name min 200, kebab 28) plus the
// 11 gap-x-3 gaps between the wide template's 12 tracks (zero-width tracks
// still carry gaps).
const FIXED_TRACKS_WIDTH = 268 + 11 * 12;

function columnTrackVars(
  isVisible: (key: AgentColumnKey) => boolean,
): React.CSSProperties {
  const width = (key: AgentColumnKey) =>
    isVisible(key) ? `${COLUMN_WIDTHS[key]}px` : "0px";
  const minWidth =
    FIXED_TRACKS_WIDTH +
    (Object.keys(COLUMN_WIDTHS) as AgentColumnKey[]).reduce(
      (sum, key) => sum + (isVisible(key) ? COLUMN_WIDTHS[key] : 0),
      0,
    );
  return {
    "--agc-status-mobile": isVisible("status") ? "96px" : "0px",
    "--agc-status-desktop": width("status"),
    "--agc-owner": width("owner"),
    "--agc-runtime": width("runtime"),
    "--agc-lastactive": width("lastActive"),
    "--agc-runs": width("runs"),
    "--agc-model": width("model"),
    "--agc-created": width("created"),
    "--agc-minw": `${minWidth}px`,
  } as React.CSSProperties;
}

export interface AgentListRow {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  activity: AgentActivity | null;
  runCount: number;
  /** Days since the last bucket with runs; null = nothing in the window. */
  lastActiveDays: number | null;
  owner: MemberWithUser | null;
  isOwnedByMe: boolean;
  canManage: boolean;
}

// Most recent activity bucket with runs, as "days ago" (0 = today).
// Day-granularity by design — derived from the same 30-day buckets the
// detail page charts, no extra API.
function lastActiveDaysAgo(activity: AgentActivity | null): number | null {
  if (!activity) return null;
  for (let i = activity.buckets.length - 1; i >= 0; i--) {
    const bucket = activity.buckets[i];
    if (bucket && bucket.total > 0) return activity.buckets.length - 1 - i;
  }
  return null;
}

export interface AgentsPageProps {
  /** Desktop-only daemon wiring, currently unused by the list (kept for
   *  platform-layer compatibility; the runtime filter lists runtimes by
   *  name rather than grouped machines). */
  localDaemonId?: string | null;
  localMachineName?: string | null;
  hasLocalMachine?: boolean;
}

// ---------------------------------------------------------------------------
// Page header
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onCreate,
}: {
  totalCount: number;
  onCreate: () => void;
}) {
  const { t } = useT("agents");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <Bot className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          {t(($) => $.page.tagline)}{" "}
          <a
            href="https://multica.ai/docs/agents"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            {t(($) => $.page.learn_more)}
          </a>
        </p>
      </div>
      {/* Quiet chrome button (outline, icon-only below md) — primary is
          reserved for the empty state's CTA. */}
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-8 w-8 gap-1 px-0 md:w-auto md:px-2.5"
        aria-label={t(($) => $.page.new_agent)}
        onClick={onCreate}
      >
        <Plus className="h-3.5 w-3.5" />
        <span className="hidden md:inline">{t(($) => $.page.new_agent)}</span>
      </Button>
    </PageHeader>
  );
}

function ListError({
  onCreate,
  listError,
  onRetry,
}: {
  onCreate: () => void;
  listError: unknown;
  onRetry: () => void;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar totalCount={0} onCreate={onCreate} />
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
        <AlertCircle className="h-8 w-8 text-destructive" />
        <div>
          <p className="text-sm font-medium">
            {t(($) => $.page.list_load_failed)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {listError instanceof Error
              ? listError.message
              : t(($) => $.page.list_load_failed_default)}
          </p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          {t(($) => $.page.try_again)}
        </Button>
      </div>
    </div>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("agents");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Bot className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">
        {t(($) => $.empty.title)}
      </h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.empty.description)}
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_agent)}
      </Button>
    </div>
  );
}

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
    <ListGridCell className="hidden justify-center px-0 @2xl:flex">
      <button
        type="button"
        aria-pressed={checked}
        onClick={(e) => {
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

// Two-line identity cell: avatar left, name + description right. The
// documented exception to the single-line rule — agents are few and
// identity-rich, so this is the "team roster" form (GitHub org members,
// Slack member list).
function NameCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const { agent, isOwnedByMe } = row;
  const isArchived = !!agent.archived_at;
  const isPrivate = agent.visibility === "private";
  return (
    <ListGridCell className="gap-3">
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={32}
        className={`shrink-0 rounded-md ${isArchived ? "opacity-50 grayscale" : ""}`}
        showStatusDot
      />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className={`min-w-0 truncate text-sm font-medium ${
              isArchived ? "text-muted-foreground" : ""
            }`}
          >
            {agent.name}
          </span>
          {isPrivate && !isArchived && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
                }
              />
              <TooltipContent>{VISIBILITY_TOOLTIP.private}</TooltipContent>
            </Tooltip>
          )}
          {isOwnedByMe && (
            <span className="shrink-0 rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground">
              {t(($) => $.row.you)}
            </span>
          )}
        </div>
        {agent.description ? (
          <div className="mt-0.5 truncate text-xs text-muted-foreground">
            {agent.description}
          </div>
        ) : null}
      </div>
    </ListGridCell>
  );
}

// Availability dot + label, with the workload folded in as a suffix
// ("Online · 2 tasks") — a 0-2 integer doesn't earn its own column.
function StatusCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const { agent, presence } = row;
  if (agent.archived_at) {
    return (
      <ListGridCell>
        <span className="text-xs text-muted-foreground/60">
          {t(($) => $.row.archived)}
        </span>
      </ListGridCell>
    );
  }
  if (!presence) {
    return (
      <ListGridCell>
        <span className="text-xs text-muted-foreground/40">—</span>
      </ListGridCell>
    );
  }
  const visual = availabilityConfig[presence.availability];
  const active = presence.runningCount + presence.queuedCount;
  return (
    <ListGridCell className="gap-1.5">
      <span className={`size-1.5 shrink-0 rounded-full ${visual.dotClass}`} />
      <span className={`truncate text-xs ${visual.textClass}`}>
        {t(($) => $.availability[presence.availability])}
        {active > 0 && (
          <span className="text-muted-foreground">
            {" · "}
            {t(($) => $.row.task_count, { count: active })}
          </span>
        )}
      </span>
    </ListGridCell>
  );
}

// Owner = the agent's owner_id, which is set to the creator at creation and
// never transferred (so owner ≡ creator). It carries management rights, so
// the column is "Owner", not "Created by".
function OwnerCell({ row }: { row: AgentListRow }) {
  const { agent, owner } = row;
  if (!agent.owner_id) {
    return (
      <ListGridCell className="hidden @2xl:flex">
        <span className="text-xs text-muted-foreground/40">—</span>
      </ListGridCell>
    );
  }
  return (
    <ListGridCell className="hidden gap-1.5 @2xl:flex">
      <ActorAvatar actorType="member" actorId={agent.owner_id} size={18} />
      <span className="min-w-0 truncate text-xs text-muted-foreground">
        {owner?.name ?? agent.owner_id.slice(0, 8)}
      </span>
    </ListGridCell>
  );
}

function RuntimeCell({ row }: { row: AgentListRow }) {
  const runtime = row.runtime;
  return (
    <ListGridCell className="hidden @2xl:flex">
      {runtime ? (
        <span className="min-w-0 truncate text-xs text-muted-foreground">
          {runtime.name}
        </span>
      ) : (
        <span className="text-xs text-muted-foreground/40">—</span>
      )}
    </ListGridCell>
  );
}

function LastActiveCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const days = row.lastActiveDays;
  return (
    <ListGridCell className="hidden @2xl:flex">
      {days === null ? (
        <span className="truncate text-xs text-muted-foreground/40">
          {row.agent.archived_at ? "—" : t(($) => $.last_active.none)}
        </span>
      ) : (
        <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
          {days === 0
            ? t(($) => $.last_active.today)
            : t(($) => $.last_active.days_ago, { count: days })}
        </span>
      )}
    </ListGridCell>
  );
}

// ---------------------------------------------------------------------------
// Header row + skeleton
// ---------------------------------------------------------------------------

function AgentListHeader({
  sortField,
  sortDirection,
  onSort,
  allSelected,
  someSelected,
  onToggleAll,
  isColVisible,
}: {
  sortField: AgentSortField;
  sortDirection: ListGridSortDirection;
  onSort: (field: AgentSortField) => void;
  allSelected: boolean;
  someSelected: boolean;
  onToggleAll: () => void;
  isColVisible: (key: AgentColumnKey) => boolean;
}) {
  const { t } = useT("agents");
  const sorted = (field: AgentSortField) =>
    sortField === field ? sortDirection : false;
  const anySelected = allSelected || someSelected;
  return (
    <ListGridHeader>
      <div className="hidden items-center justify-center @2xl:flex">
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
        {t(($) => $.columns.agent)}
      </ListGridHeaderCell>
      {isColVisible("status") ? (
        <ListGridHeaderCell>{t(($) => $.columns.status)}</ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="px-0" />
      )}
      {isColVisible("owner") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.columns.owner)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("runtime") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.columns.runtime)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("lastActive") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          sorted={sorted("lastActive")}
          onSort={() => onSort("lastActive")}
        >
          {t(($) => $.columns.last_active)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("runs") ? (
        <ListGridHeaderCell
          className="hidden @2xl:flex"
          align="right"
          sorted={sorted("runs")}
          onSort={() => onSort("runs")}
        >
          {t(($) => $.columns.runs)}
        </ListGridHeaderCell>
      ) : (
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
      )}
      {isColVisible("model") ? (
        <ListGridHeaderCell className="hidden @2xl:flex">
          {t(($) => $.columns.model)}
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
          {t(($) => $.columns.created)}
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
        (key) => !AGENT_DEFAULT_HIDDEN_COLUMNS.includes(key),
      )}
    >
      <ListGridHeader>
        <span aria-hidden="true" className="hidden @2xl:inline" />
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell>
          <Skeleton className="h-3 w-12" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-14" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-14" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-14" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden @2xl:flex">
          <Skeleton className="h-3 w-10" />
        </ListGridHeaderCell>
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
        <span aria-hidden="true" />
      </ListGridHeader>
      {Array.from({ length: 5 }).map((_, i) => (
        <ListGridRow key={i} className="h-16 hover:bg-transparent">
          <span aria-hidden="true" className="hidden @2xl:inline" />
          <ListGridCell className="gap-3">
            <Skeleton className="size-8 rounded-md" />
            <div className="min-w-0 flex-1 space-y-1.5">
              <Skeleton className="h-3.5 w-32 max-w-full" />
              <Skeleton className="h-3 w-48 max-w-full" />
            </div>
          </ListGridCell>
          <ListGridCell>
            <Skeleton className="h-3 w-16" />
          </ListGridCell>
          <ListGridCell className="hidden gap-1.5 @2xl:flex">
            <Skeleton className="size-5 rounded-full" />
            <Skeleton className="h-3 w-12" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-16" />
          </ListGridCell>
          <ListGridCell className="hidden @2xl:flex">
            <Skeleton className="h-3 w-12" />
          </ListGridCell>
          <ListGridCell className="hidden justify-end @2xl:flex">
            <Skeleton className="h-3 w-8" />
          </ListGridCell>
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <ListGridCell className="hidden px-0 @2xl:flex" />
          <span aria-hidden="true" />
        </ListGridRow>
      ))}
    </ListGrid>
  );
}

// ---------------------------------------------------------------------------
// Batch toolbar — archive (with confirm; archiving cancels active tasks) and
// restore, mirroring the single-row actions. No delete: the API has none.
// ---------------------------------------------------------------------------

function AgentBatchToolbar({
  rows,
  onClear,
}: {
  rows: AgentListRow[];
  onClear: () => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [busy, setBusy] = useState(false);

  if (rows.length === 0) return null;

  const allManageable = rows.every((r) => r.canManage);
  const anyActive = rows.some((r) => !r.agent.archived_at);
  const anyArchived = rows.some((r) => !!r.agent.archived_at);

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });

  const runBatch = async (
    fn: (id: string) => Promise<unknown>,
    targets: AgentListRow[],
  ) => {
    setBusy(true);
    try {
      for (const row of targets) {
        await fn(row.agent.id);
      }
      invalidate();
      onClear();
    } catch (e) {
      invalidate();
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {/* Anchored to the page root (relative), NOT the viewport. */}
      <div className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
          <span className="text-sm font-medium">
            {t(($) => $.actions.selected, { count: rows.length })}
          </span>
          <button
            type="button"
            aria-label={t(($) => $.actions.clear_selection)}
            onClick={onClear}
            className="rounded p-0.5 transition-colors hover:bg-accent"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        {anyActive && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!allManageable || busy}
            onClick={() => setConfirmArchive(true)}
          >
            <Archive className="mr-1 size-3.5" />
            {t(($) => $.row_actions.archive)}
          </Button>
        )}
        {anyArchived && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!allManageable || busy}
            onClick={() =>
              runBatch(
                (id) => api.restoreAgent(id),
                rows.filter((r) => !!r.agent.archived_at),
              )
            }
          >
            <ArchiveRestore className="mr-1 size-3.5" />
            {t(($) => $.row_actions.restore)}
          </Button>
        )}
      </div>

      <Dialog open={confirmArchive} onOpenChange={setConfirmArchive}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.row_actions.archive_dialog_title, {
                name:
                  rows.length === 1 && rows[0]
                    ? rows[0].agent.name
                    : String(rows.length),
              })}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.row_actions.archive_dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => setConfirmArchive(false)}
            >
              {t(($) => $.row_actions.archive_dialog_cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={async () => {
                await runBatch(
                  (id) => api.archiveAgent(id),
                  rows.filter((r) => !r.agent.archived_at),
                );
                setConfirmArchive(false);
              }}
            >
              {busy ? (
                <Loader2 className="mr-1 size-3.5 animate-spin" />
              ) : null}
              {t(($) => $.row_actions.archive_dialog_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function AgentsPage(_props: AgentsPageProps = {}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const rowLink = useRowLink();
  const qc = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);

  const {
    data: agents = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runCountsRaw = [] } = useQuery(agentRunCounts30dOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);

  const [showCreate, setShowCreate] = useState(false);
  const [duplicateTemplate, setDuplicateTemplate] = useState<Agent | null>(
    null,
  );
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );

  const rawScope = useAgentsViewStore((s) => s.scope);
  const scope = AGENT_SCOPES.includes(rawScope) ? rawScope : "mine";
  const setScope = useAgentsViewStore((s) => s.setScope);
  const sortField = useAgentsViewStore((s) => s.sortField);
  const sortDirection = useAgentsViewStore((s) => s.sortDirection);
  const hiddenColumns = useAgentsViewStore((s) => s.hiddenColumns);
  const filters = useAgentsViewStore((s) => s.filters);
  const handleSort = useAgentsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useAgentsViewStore((s) => s.setSortField);
  const setSortDirection = useAgentsViewStore((s) => s.setSortDirection);
  const toggleColumn = useAgentsViewStore((s) => s.toggleColumn);
  const toggleFilter = useAgentsViewStore((s) => s.toggleFilter);
  const clearFilters = useAgentsViewStore((s) => s.clearFilters);

  const isColVisible = (key: AgentColumnKey) => !hiddenColumns.includes(key);

  const toggleSelected = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const runtimesById = useMemo(() => {
    const m = new Map<string, AgentRuntime>();
    for (const r of runtimes) m.set(r.id, r);
    return m;
  }, [runtimes]);

  const runCountsById = useMemo(() => {
    const m = new Map<string, number>();
    for (const r of runCountsRaw) m.set(r.agent_id, r.run_count);
    return m;
  }, [runCountsRaw]);

  const membersById = useMemo(() => {
    const m = new Map<string, MemberWithUser>();
    for (const mem of members) m.set(mem.user_id, mem);
    return m;
  }, [members]);

  const isWorkspaceAdmin = useMemo(() => {
    if (!currentUser) return false;
    const me = members.find((m) => m.user_id === currentUser.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser]);

  // Scope counts come from the FULL set (filters never affect them).
  // Archived ignores the ownership lens (see the view store comment).
  const scopeCounts = useMemo<Record<AgentsScope, number>>(() => {
    let mine = 0;
    let all = 0;
    let archived = 0;
    for (const a of agents) {
      if (a.archived_at) {
        archived++;
        continue;
      }
      all++;
      if (currentUser && a.owner_id === currentUser.id) mine++;
    }
    return { mine, all, archived };
  }, [agents, currentUser]);

  // Rows within the current scope, unfiltered, fully assembled — the
  // toolbar's option lists and the "n / total" denominator derive from
  // this; cells never pull their own queries.
  const scopeRows = useMemo<AgentListRow[]>(() => {
    const inScope = agents.filter((a) => {
      if (scope === "archived") return !!a.archived_at;
      if (a.archived_at) return false;
      if (scope === "mine") {
        return !!currentUser && a.owner_id === currentUser.id;
      }
      return true;
    });
    return inScope.map((agent) => {
      const isOwner = !!currentUser?.id && agent.owner_id === currentUser.id;
      const activity = activityMap.get(agent.id) ?? null;
      return {
        agent,
        runtime: runtimesById.get(agent.runtime_id) ?? null,
        presence: presenceMap.get(agent.id) ?? null,
        activity,
        runCount: runCountsById.get(agent.id) ?? 0,
        lastActiveDays: lastActiveDaysAgo(activity),
        owner: agent.owner_id ? membersById.get(agent.owner_id) ?? null : null,
        isOwnedByMe: isOwner,
        canManage: isWorkspaceAdmin || isOwner,
      };
    });
  }, [
    agents,
    scope,
    currentUser,
    runtimesById,
    membersById,
    presenceMap,
    activityMap,
    runCountsById,
    isWorkspaceAdmin,
  ]);

  // Visible rows: filters, then sort.
  const rows = useMemo<AgentListRow[]>(() => {
    const filtered = scopeRows.filter((row) => {
      if (
        filters.availability.length > 0 &&
        (!row.presence ||
          !filters.availability.includes(row.presence.availability))
      ) {
        return false;
      }
      if (
        filters.runtimes.length > 0 &&
        !filters.runtimes.includes(row.agent.runtime_id)
      ) {
        return false;
      }
      if (
        filters.owners.length > 0 &&
        (!row.agent.owner_id || !filters.owners.includes(row.agent.owner_id))
      ) {
        return false;
      }
      if (
        filters.models.length > 0 &&
        !filters.models.includes(row.agent.model)
      ) {
        return false;
      }
      return true;
    });

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.agent.name.localeCompare(b.agent.name) * dir;
      }
      if (sortField === "runs") {
        return (a.runCount - b.runCount) * dir ||
          a.agent.name.localeCompare(b.agent.name);
      }
      if (sortField === "created") {
        return (
          (Date.parse(a.agent.created_at) - Date.parse(b.agent.created_at)) *
          dir
        );
      }
      // lastActive: smaller daysAgo = more recent. "desc" (the default)
      // means most recently active first; never-active rows sort last in
      // both directions. Run count breaks ties.
      const av = a.lastActiveDays ?? Number.POSITIVE_INFINITY;
      const bv = b.lastActiveDays ?? Number.POSITIVE_INFINITY;
      const byDays = sortDirection === "desc" ? av - bv : bv - av;
      return (
        byDays || b.runCount - a.runCount ||
        a.agent.name.localeCompare(b.agent.name)
      );
    });
    return filtered;
  }, [scopeRows, filters, sortField, sortDirection]);

  // Row virtualization — headless math, offsets as padding on the rows
  // wrapper, fixed-height rows. The scroll element is the SINGLE outer
  // scroller (both axes): splitting horizontal scrolling (wrapper) from
  // vertical scrolling (an inner element) connected by an h-full
  // percentage bridge caused a non-converging layout loop (flickering
  // double scrollbars) and clipped the last row under the horizontal
  // scrollbar. The sticky header pins inside this scroller; the vertical
  // scrollbar spans the full pane height (Linear's structure).
  const listScrollRef = useRef<HTMLDivElement | null>(null);
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => listScrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
  });

  const handleCreate = async (data: CreateAgentRequest): Promise<Agent> => {
    const agent = await api.createAgent(data);
    qc.setQueryData<Agent[]>(workspaceKeys.agents(wsId), (current = []) => {
      const exists = current.some((a) => a.id === agent.id);
      return exists
        ? current.map((a) => (a.id === agent.id ? agent : a))
        : [...current, agent];
    });
    setShowCreate(false);
    setDuplicateTemplate(null);
    navigation.push(paths.agentDetail(agent.id));
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    return agent;
  };

  const handleDuplicate = useCallback((agent: Agent) => {
    setDuplicateTemplate(agent);
    setShowCreate(true);
  }, []);

  const selectedRows = rows.filter((row) => selectedIds.has(row.agent.id));
  const allSelected = rows.length > 0 && selectedRows.length === rows.length;
  const someSelected = selectedRows.length > 0 && !allSelected;
  const handleToggleAll = () => {
    setSelectedIds(
      allSelected ? new Set() : new Set(rows.map((r) => r.agent.id)),
    );
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

  if (listError) {
    return (
      <ListError
        onCreate={() => setShowCreate(true)}
        listError={listError}
        onRetry={() => refetchList()}
      />
    );
  }

  const totalCount = agents.filter((a) => !a.archived_at).length;
  const showEmpty = !isLoading && agents.length === 0;

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalCount}
        onCreate={() => setShowCreate(true)}
      />

      {isLoading ? (
        <div className="flex-1 overflow-y-auto @container">
          <LoadingSkeleton />
        </div>
      ) : showEmpty ? (
        <div className="flex flex-1 items-center justify-center">
          <EmptyState onCreate={() => setShowCreate(true)} />
        </div>
      ) : (
        <>
          <AgentListToolbar
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
            members={members}
            visibleCount={rows.length}
          />
          <div
            ref={listScrollRef}
            className="min-h-0 flex-1 overflow-auto @container"
          >
            <ListGrid
              className={`${GRID_COLS} @2xl:min-w-[var(--agc-minw)]`}
              style={columnTrackVars(isColVisible)}
            >
              <AgentListHeader
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
                    {t(($) => $.no_matches.title)}
                  </div>
                )}
                {virtualItems.map((vi) => {
                  const row = rows[vi.index];
                  if (!row) return null;
                  return (
                    <ListGridRow
                      key={row.agent.id}
                      className={`h-16 cursor-pointer ${
                        selectedIds.has(row.agent.id) ? "bg-accent/30" : ""
                      }`}
                      {...rowLink(paths.agentDetail(row.agent.id))}
                    >
                      <CheckboxCell
                        checked={selectedIds.has(row.agent.id)}
                        onToggle={() => toggleSelected(row.agent.id)}
                      />
                      <NameCell row={row} />
                      {isColVisible("status") ? (
                        <StatusCell row={row} />
                      ) : (
                        <ListGridCell className="px-0" />
                      )}
                      {isColVisible("owner") ? (
                        <OwnerCell row={row} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("runtime") ? (
                        <RuntimeCell row={row} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("lastActive") ? (
                        <LastActiveCell row={row} />
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("runs") ? (
                        <ListGridCell className="hidden justify-end font-mono text-xs tabular-nums text-muted-foreground @2xl:flex">
                          {row.runCount.toLocaleString()}
                        </ListGridCell>
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("model") ? (
                        <ListGridCell className="hidden @2xl:flex">
                          <span className="min-w-0 truncate text-xs text-muted-foreground">
                            {row.agent.model || "—"}
                          </span>
                        </ListGridCell>
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      {isColVisible("created") ? (
                        <ListGridCell className="hidden whitespace-nowrap text-xs tabular-nums text-muted-foreground @2xl:flex">
                          {new Date(
                            row.agent.created_at,
                          ).toLocaleDateString()}
                        </ListGridCell>
                      ) : (
                        <ListGridCell className="hidden px-0 @2xl:flex" />
                      )}
                      <ListGridCell className="justify-end px-0">
                        <span
                          onClick={(e) => e.stopPropagation()}
                          className="flex items-center"
                        >
                          <AgentRowActions
                            agent={row.agent}
                            presence={row.presence}
                            canManage={row.canManage}
                            onDuplicate={handleDuplicate}
                          />
                        </span>
                      </ListGridCell>
                    </ListGridRow>
                  );
                })}
              </ListGridBody>
            </ListGrid>
          </div>
        </>
      )}

      <AgentBatchToolbar
        rows={selectedRows}
        onClear={() => setSelectedIds(new Set())}
      />

      {showCreate && (
        <CreateAgentDialog
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          members={members}
          currentUserId={currentUser?.id ?? null}
          template={duplicateTemplate}
          onClose={() => {
            setShowCreate(false);
            setDuplicateTemplate(null);
          }}
          onCreate={handleCreate}
        />
      )}
    </div>
  );
}

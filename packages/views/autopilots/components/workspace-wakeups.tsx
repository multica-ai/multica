"use client";

import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { AlarmClock, Bell, Clock3, AlertCircle, ListChecks, TriangleAlert } from "@multica/ui/icons";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  workspaceWakeupsInfiniteOptions,
  useDisableWorkspaceWakeups,
  useDisableIssueWakeup,
  useEnableIssueWakeup,
  useUpdateIssueSystemWakeup,
} from "@multica/core/issues/wakeups";
import type {
  Issue,
  WorkspaceWakeup,
  WorkspaceWakeupFilters,
} from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../../i18n";
import {
  CollectionCreateButton,
  CollectionFilterButton,
  CollectionPageHeader,
  CollectionPageState,
  CollectionResetButton,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionBatchBar,
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
} from "../../layout/collection-list";
import { ActorAvatar } from "../../common/actor-avatar";
import { TranscriptButton } from "../../common/task-transcript";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { WakeupInstructionEditor } from "../../issues/components/wakeup-instruction-editor";
import { WakeupControl } from "../../issues/components/wakeup-control";
import { WakeupCreateForm } from "../../issues/components/wakeup-create";
import { conditionIcon } from "../../issues/components/wakeups-section";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import {
  isActiveWakeupRun,
  useWakeupText,
} from "../../issues/components/wakeup-presentation";
import { wakeupCountsOptions } from "./wakeups-tab";

const SCOPES = ["active", "paused", "disabled", "ended", "all"] as const;

type WakeupFilters = Omit<WorkspaceWakeupFilters, "offset">;

// Per-row controls (prompt editing, the last run's transcript) stay out of the
// way until the pointer or focus is on the row.
const rowIntent =
  "opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100";

const rowId = (row: WorkspaceWakeup) => (row.source === "system" ? `system-${row.id}` : row.id);

function useSystemTitle() {
  const { t } = useT("issues");
  return (row: WorkspaceWakeup) =>
    row.system_stage != null
      ? t(($) => $.wakeups.system.title_stage, { stage: row.system_stage })
      : t(($) => $.wakeups.system.title_all);
}

/** Identifier and title, then what wakes it as the row's description. */
function IssueCell({ row }: { row: WorkspaceWakeup }) {
  const text = useWakeupText();
  const systemTitle = useSystemTitle();
  const system = row.source === "system";
  const Icon = system
    ? ListChecks
    : (conditionIcon(row.condition) ?? (row.kind === "event" ? Bell : Clock3));
  const trigger = system ? systemTitle(row) : text.trigger(row);
  const detail = system ? undefined : row.kind === "cron" ? row.timezone : text.frequency(row);
  return (
    <CollectionNameCell
      icon={<Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />}
      title={
        <span title={`${row.issue_identifier} ${row.issue_title}`}>
          <span className="mr-2 font-normal tabular-nums text-muted-foreground">
            {row.issue_identifier}
          </span>
          {row.issue_title}
        </span>
      }
      description={<span title={detail ? `${trigger} · ${detail}` : trigger}>{trigger}</span>}
    />
  );
}

function AgentCell({ row }: { row: WorkspaceWakeup }) {
  const { t } = useT("autopilots");
  if (!row.agent_id) return <>{t(($) => $.wakeups.no_target)}</>;
  return (
    <>
      <ActorAvatar actorType="agent" actorId={row.agent_id} name={row.agent_name} size="sm" />
      <span className="truncate">
        {row.source === "system"
          ? t(($) => $.wakeups.assignee_target, { name: row.agent_name })
          : row.agent_name}
      </span>
    </>
  );
}

/** Who a rule came from: a member, an agent run, or the platform. */
function SourceCell({ row }: { row: WorkspaceWakeup }) {
  const { t } = useT("autopilots");
  if (row.source === "system") {
    return (
      <span className="rounded-xs bg-muted px-1.5 text-caption leading-5">
        {t(($) => $.wakeups.sources.system)}
      </span>
    );
  }
  const agent = row.source === "agent" && row.source_agent_id;
  return (
    <>
      <ActorAvatar
        actorType={agent ? "agent" : "member"}
        actorId={agent ? (row.source_agent_id ?? "") : ""}
        name={agent ? (row.source_agent_name ?? "") : (row.created_by_name ?? "")}
        size="sm"
      />
      <span className="truncate">{agent ? row.source_agent_name : row.created_by_name}</span>
    </>
  );
}

function EndsCell({ row }: { row: WorkspaceWakeup }) {
  const { t } = useT("autopilots");
  const { t: ti } = useT("issues");
  const locale = useLocale();
  const text = useWakeupText();
  const viewTZ = useViewingTimezone();
  if (row.source === "system") return <>{t(($) => $.wakeups.until_issue_ends)}</>;
  // While enabled, a rule reads as when it ends; otherwise as its state.
  const ends = row.enabled && !row.issue_closed
    ? (text.ending(row) ?? (row.mode === "once" ? t(($) => $.wakeups.fires_once) : text.state(row)))
    : (text.paused(row) ?? text.state(row, row.issue_closed));
  return (
    <span
      className={cn("flex min-w-0 items-center gap-1.5", row.paused_reason && "text-warning")}
      title={
        row.next_fire_at && row.enabled
          ? `${new Date(row.next_fire_at).toLocaleString(locale, { timeZone: viewTZ })} · ${viewTZ}`
          : undefined
      }
    >
      <span className="truncate">{ends}</span>
      {row.last_error && (
        <span className="shrink-0 text-destructive">{ti(($) => $.wakeups.needs_attention)}</span>
      )}
    </span>
  );
}

/** Runs in the last seven days, and the current run when there is one. */
function RunsCell({ row }: { row: WorkspaceWakeup }) {
  const { t } = useT("autopilots");
  const { t: ti } = useT("issues");
  const text = useWakeupText();
  const status = row.task?.status;
  const active = isActiveWakeupRun(status);
  return (
    <span className="flex min-w-0 items-center gap-1 tabular-nums">
      <span className="text-foreground">{row.runs_7d}</span>
      {row.task && active && (
        <>
          <span aria-hidden="true">·</span>
          <span
            className="truncate text-primary"
            title={row.active_runs > 1 ? t(($) => $.wakeups.active_runs, { count: row.active_runs }) : undefined}
          >
            {text.runState(status)}
          </span>
        </>
      )}
      {row.task && (
        <span className={cn("shrink-0", !active && rowIntent)}>
          <TranscriptButton task={row.task} agentName={row.agent_name} title={ti(($) => $.wakeups.last_run)} isLive={active} />
        </span>
      )}
    </span>
  );
}

/** The sub-issue system rule's per-issue switch. */
function SystemWakeupControl({ row, busy }: { row: WorkspaceWakeup; busy: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const update = useUpdateIssueSystemWakeup(wsId, row.issue_id);
  return (
    <Switch
      size="sm"
      checked={row.enabled}
      disabled={busy || update.isPending}
      aria-label={`${row.issue_identifier} · ${t(($) => $.wakeups.system.toggle)}`}
      onCheckedChange={(enabled) =>
        update.mutate({ rule: "child_done", enabled }, { onError: () => toast.error(t(($) => $.wakeups.system.save_error)) })
      }
    />
  );
}

function WakeupControls({ row, busy }: { row: WorkspaceWakeup; busy: boolean }) {
  const { t } = useT("autopilots");
  const { t: ti } = useT("issues");
  const wsId = useWorkspaceId();
  const text = useWakeupText();
  const disable = useDisableIssueWakeup(wsId, row.issue_id);
  const enable = useEnableIssueWakeup(wsId, row.issue_id);
  return (
    <span
      className="flex items-center gap-1"
      title={
        !row.can_manage
          ? t(($) => $.wakeups.read_only)
          : row.issue_closed
            ? ti(($) => $.wakeups.closed_hint)
            : undefined
      }
    >
      <span className={rowIntent}>
        <WakeupInstructionEditor
          workspaceId={wsId}
          issueId={row.issue_id}
          wakeupId={row.id}
          disabled={busy || !row.can_manage}
          triggerStyle="icon"
        />
      </span>
      <WakeupControl
        size="xs"
        wakeup={row}
        task={row.task ?? undefined}
        closed={row.issue_closed}
        pending={busy || !row.can_manage || disable.isPending || enable.isPending}
        onDisable={() =>
          disable.mutate(row.id, {
            onError: (err) => toast.error(text.error(err, ti(($) => $.wakeups.disable_error))),
          })
        }
        onEnable={async (input = {}) => {
          await enable.mutateAsync({ id: row.id, revision: row.revision ?? 0, ...input });
        }}
      />
    </span>
  );
}

/**
 * "New wakeup" from the workspace list: a wakeup belongs to one issue, so pick
 * it first, then fill in the same form as the issue sidebar.
 */
export function WorkspaceWakeupCreate({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const [issue, setIssue] = useState<Issue | null>(null);
  // The picker reports a selection and then closes itself; only a close
  // without a selection ends the flow.
  const picked = useRef(false);
  const close = () => {
    picked.current = false;
    setIssue(null);
    onOpenChange(false);
  };
  return (
    <>
      <IssuePickerModal
        open={open && !issue}
        onOpenChange={(next) => {
          if (!next && !picked.current) close();
        }}
        title={t(($) => $.wakeups.create_pick_issue)}
        description={t(($) => $.wakeups.create_pick_issue_description)}
        excludeIds={[]}
        isSelectable={(candidate) => candidate.status !== "done" && candidate.status !== "cancelled"}
        onSelect={(candidate) => {
          picked.current = true;
          setIssue(candidate);
        }}
      />
      <Dialog
        open={open && !!issue}
        onOpenChange={(next) => {
          if (!next) close();
        }}
      >
        <DialogContent className="sm:max-w-[460px]">
          {issue && (
            <WakeupCreateForm
              workspaceId={wsId}
              issueId={issue.id}
              defaultAgentId={issue.assignee_type === "agent" ? (issue.assignee_id ?? "") : ""}
              inDialog
              onClose={close}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

/** One filter dimension of the header's filter menu; the first item is "any". */
function FilterSub<T extends string>({
  label,
  value,
  items,
  onChange,
}: {
  label: string;
  value: T;
  items: { value: T; label: string }[];
  onChange: (value: T) => void;
}) {
  const current = value !== items[0]?.value ? items.find((item) => item.value === value) : undefined;
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <span className="flex-1">{label}</span>
        {current && (
          <span className="max-w-32 truncate pl-3 text-caption font-medium text-primary">{current.label}</span>
        )}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="max-h-72 w-auto min-w-44 overflow-y-auto">
        <DropdownMenuRadioGroup
          value={value}
          onValueChange={(next) => {
            const item = items.find((candidate) => candidate.value === next);
            if (item) onChange(item.value);
          }}
        >
          {items.map((item) => (
            <DropdownMenuRadioItem key={item.value} value={item.value}>
              <span className="truncate">{item.label}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}

export function WorkspaceWakeups() {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const text = useWakeupText();
  const [filters, setFilters] = useState<WakeupFilters>({
    scope: "active",
    kind: "all",
    source: "",
    search: "",
    agent_id: "",
    limit: 50,
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmation, setConfirmation] = useState<WorkspaceWakeup[]>([]);
  const [batchResult, setBatchResult] = useState<{
    failed: string[];
    succeeded: number;
  } | null>(null);
  const query = useInfiniteQuery(workspaceWakeupsInfiniteOptions(wsId, filters));
  const page = query.data?.pages[0];
  // The newest rule the platform paused, for the banner above the list.
  const pausedQuery = useQuery({
    ...wakeupCountsOptions(wsId),
    enabled: !!wsId && (page?.counts.paused ?? 0) > 0,
  });
  const batch = useDisableWorkspaceWakeups(wsId);
  // Offset pages can overlap when rules change between fetches.
  const rows = useMemo(() => {
    const seen = new Set<string>();
    return (query.data?.pages ?? []).flatMap((p) =>
      p.items.filter((row) => {
        const id = rowId(row);
        if (seen.has(id)) return false;
        seen.add(id);
        return true;
      }),
    );
  }, [query.data]);
  const isSelectable = (row: WorkspaceWakeup) =>
    row.enabled && row.can_manage && row.source !== "system";
  const picked = rows.filter((row) => isSelectable(row) && selected.has(row.id));
  const change = useCallback((patch: Partial<WakeupFilters>) => {
    setFilters((prev) => ({ ...prev, ...patch }));
    setSelected(new Set());
    setBatchResult(null);
  }, []);
  const kinds = [
    { value: "all" as const, label: t(($) => $.wakeups.all_triggers) },
    { value: "event" as const, label: t(($) => $.wakeups.event) },
    { value: "at" as const, label: t(($) => $.wakeups.at) },
    { value: "recurring" as const, label: t(($) => $.wakeups.recurring) },
  ];
  const agents = [
    { value: "", label: t(($) => $.wakeups.all_agents) },
    ...(page?.agents ?? []).map((a) => ({ value: a.id, label: a.name })),
  ];
  const sources = [
    { value: "" as const, label: t(($) => $.wakeups.sources.all) },
    { value: "member" as const, label: t(($) => $.wakeups.sources.member) },
    { value: "agent" as const, label: t(($) => $.wakeups.sources.agent) },
    { value: "system" as const, label: t(($) => $.wakeups.sources.system) },
  ];
  const pausedCount = page?.counts.paused ?? 0;
  const latestPaused = pausedQuery.data?.items[0];
  const filtered = filters.scope !== "all" || filters.kind !== "all" || !!filters.source || !!filters.agent_id;
  const filterCount = [filters.kind !== "all", !!filters.source, !!filters.agent_id].filter(Boolean).length;
  const busy = batch.isPending;
  const columns: CollectionColumn<WorkspaceWakeup>[] = [
    { key: "issue", header: t(($) => $.wakeups.issue), cell: (row) => <IssueCell row={row} /> },
    {
      key: "agent",
      header: t(($) => $.wakeups.target_agent),
      width: 128,
      cell: (row) => <AgentCell row={row} />,
    },
    {
      key: "source",
      header: t(($) => $.wakeups.source),
      width: 112,
      cell: (row) => <SourceCell row={row} />,
    },
    {
      key: "ends",
      header: t(($) => $.wakeups.ends),
      width: 128,
      cell: (row) => <EndsCell row={row} />,
    },
    {
      key: "runs",
      header: t(($) => $.wakeups.runs_7d),
      width: 88,
      interactive: true,
      cell: (row) => <RunsCell row={row} />,
    },
    {
      key: "enabled",
      header: t(($) => $.wakeups.enabled),
      width: 96,
      align: "end",
      core: true,
      interactive: true,
      cell: (row) =>
        row.source === "system" ? (
          <SystemWakeupControl row={row} busy={busy} />
        ) : (
          <WakeupControls row={row} busy={busy} />
        ),
    },
  ];
  let body: ReactNode;
  if (query.isError) {
    body = (
      <CollectionPageState
        icon={AlertCircle}
        tone="destructive"
        role="alert"
        title={t(($) => $.wakeups.load_error)}
        actions={
          <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
            {t(($) => $.page.retry)}
          </Button>
        }
      />
    );
  } else if (query.isPending) {
    body = <CollectionPageState icon={Clock3} title={t(($) => $.wakeups.loading)} />;
  } else if (!rows.length) {
    const hasAny = (page?.counts.all ?? 0) > 0;
    body = (
      <CollectionPageState
        icon={Bell}
        title={t(($) => (hasAny || filtered ? $.wakeups.empty_filtered : $.wakeups.empty))}
        actions={
          filtered && hasAny ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => change({ scope: "all", kind: "all", source: "", agent_id: "" })}
            >
              {t(($) => $.wakeups.clear_filters)}
            </Button>
          ) : undefined
        }
      />
    );
  } else {
    body = (
      <CollectionList
        label={t(($) => $.wakeups.scopes[filters.scope])}
        columns={columns}
        rows={rows}
        getRowId={rowId}
        getRowHref={(row) => paths.issueDetail(row.issue_id)}
        getRowTitle={(row) => `${row.issue_identifier} ${row.issue_title}`}
        selection={{
          selectedIds: selected,
          onChange: (next) => {
            if (!batch.isPending) setSelected(new Set(next));
          },
          isSelectable,
          getLabel: (row) =>
            t(($) => $.wakeups.select_row, { issue: row.issue_identifier, agent: row.agent_name }),
          allLabel: t(($) => $.wakeups.select_page),
        }}
        hasMore={query.hasNextPage}
        loadingMore={query.isFetchingNextPage}
        onLoadMore={() => void query.fetchNextPage()}
      />
    );
  }
  return (
    <>
      <CollectionPageHeader
        icon={AlarmClock}
        title={t(($) => $.wakeups.scopes[filters.scope])}
        menu={
          <CollectionScopeItems
            value={filters.scope}
            options={SCOPES.map((scope) => ({
              value: scope,
              label: t(($) => $.wakeups.scopes[scope]),
              count: page?.counts[scope],
            }))}
            onChange={(scope) => {
              if (!batch.isPending) change({ scope });
            }}
          />
        }
        tools={
          <>
            <DropdownMenu>
              <DropdownMenuTrigger
                disabled={batch.isPending}
                render={<CollectionFilterButton count={filterCount} />}
              />
              <DropdownMenuContent align="start" className="w-auto min-w-44">
                <FilterSub label={t(($) => $.wakeups.source)} value={filters.source} items={sources} onChange={(source) => change({ source })} />
                <FilterSub label={t(($) => $.wakeups.trigger)} value={filters.kind} items={kinds} onChange={(kind) => change({ kind })} />
                <FilterSub label={t(($) => $.wakeups.target_agent)} value={filters.agent_id} items={agents} onChange={(agent_id) => change({ agent_id })} />
              </DropdownMenuContent>
            </DropdownMenu>
            {filterCount > 0 && (
              <CollectionResetButton
                onClick={() => change({ kind: "all", source: "", agent_id: "" })}
              />
            )}
          </>
        }
        actions={
          <CollectionCreateButton label={t(($) => $.wakeups.create)} onClick={() => setCreateOpen(true)} />
        }
      />
      {pausedCount > 0 && latestPaused && filters.scope !== "paused" && (
        <div
          role="status"
          className="mx-4 mt-3 flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption"
        >
          <TriangleAlert className="size-4 shrink-0 text-destructive" aria-hidden="true" />
          <span className="min-w-0 flex-1">
            {t(($) => $.wakeups.banner, {
              count: pausedCount,
              issue: latestPaused.issue_identifier,
              condition: text.trigger(latestPaused),
              reason: text.pausedReason(latestPaused) ?? "",
            })}
          </span>
          <Button size="sm" variant="outline" onClick={() => change({ scope: "paused" })}>
            {t(($) => $.wakeups.banner_view)}
          </Button>
        </div>
      )}
      {batchResult && (
        <p
          role="status"
          className={`px-4 py-2 text-caption ${batchResult.failed.length ? "text-destructive" : "text-muted-foreground"}`}
        >
          {t(($) => (batchResult.failed.length ? $.wakeups.batch_partial : $.wakeups.batch_success), {
            count: batchResult.succeeded,
            succeeded: batchResult.succeeded,
            failed: batchResult.failed.length,
          })}
        </p>
      )}
      <div className="relative flex min-h-0 flex-1 flex-col">
        {body}
        <CollectionBatchBar count={picked.length} onClear={() => setSelected(new Set())}>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmation(picked)}>
            {t(($) => $.wakeups.disable_selected)}
          </Button>
        </CollectionBatchBar>
      </div>
      <Dialog
        open={confirmation.length > 0}
        onOpenChange={(open) => {
          if (!open && !batch.isPending) setConfirmation([]);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.wakeups.confirm_title, { count: confirmation.length })}</DialogTitle>
            <DialogDescription>{t(($) => $.wakeups.confirm_body)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" disabled={batch.isPending} onClick={() => setConfirmation([])}>
              {t(($) => $.wakeups.cancel)}
            </Button>
            <Button
              disabled={batch.isPending}
              onClick={async () => {
                const result = await batch.mutateAsync(confirmation);
                setBatchResult(result);
                setSelected(new Set(result.failed));
                setConfirmation([]);
              }}
            >
              {batch.isPending ? t(($) => $.wakeups.disabling) : t(($) => $.wakeups.confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <WorkspaceWakeupCreate open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

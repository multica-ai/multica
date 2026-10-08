"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, Clock3, AlertCircle, ListChecks, Search, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  workspaceWakeupsOptions,
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
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  Table,
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@multica/ui/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
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

// Per-row controls (selection, prompt editing, the last run's transcript)
// stay out of the way until the pointer or focus is on the row.
const rowIntent =
  "opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100";

/**
 * Identifier and title on one line; the whole cell opens the issue. The column
 * takes the width the others leave and truncates, so the table fits the page.
 */
function IssueCell({ row }: { row: WorkspaceWakeup }) {
  const paths = useWorkspacePaths();
  return (
    <TableCell className="w-full max-w-0">
      <AppLink
        href={paths.issueDetail(row.issue_id)}
        title={`${row.issue_identifier} ${row.issue_title}`}
        className="flex min-w-0 items-center gap-2 rounded-sm focus-visible:outline-2 focus-visible:outline-ring"
      >
        <span className="shrink-0 text-caption tabular-nums text-muted-foreground">{row.issue_identifier}</span>
        <span className="truncate">{row.issue_title}</span>
      </AppLink>
    </TableCell>
  );
}

function TriggerCell({ icon: Icon, label, detail }: { icon: typeof Bell; label: string; detail?: string }) {
  return (
    <TableCell className="max-w-64">
      <span className="flex min-w-0 items-center gap-1.5" title={detail ? `${label} · ${detail}` : label}>
        <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="truncate">{label}</span>
      </span>
    </TableCell>
  );
}

function AgentCell({ id, name, label }: { id: string; name: string; label?: string }) {
  return (
    <TableCell className="max-w-44">
      <span className="flex min-w-0 items-center gap-2">
        <ActorAvatar actorType="agent" actorId={id} name={name} size="sm" />
        <span className="truncate">{label ?? name}</span>
      </span>
    </TableCell>
  );
}

/** Who a rule came from: a member, an agent run, or the platform. */
function SourceCell({ row }: { row: WorkspaceWakeup }) {
  const { t } = useT("autopilots");
  if (row.source === "system") {
    return (
      <span className="rounded-xs bg-muted px-1.5 py-0.5 text-caption text-muted-foreground">
        {t(($) => $.wakeups.sources.system)}
      </span>
    );
  }
  const agent = row.source === "agent" && row.source_agent_id;
  return (
    <span className="flex min-w-0 items-center gap-2">
      <ActorAvatar
        actorType={agent ? "agent" : "member"}
        actorId={agent ? (row.source_agent_id ?? "") : ""}
        name={agent ? (row.source_agent_name ?? "") : (row.created_by_name ?? "")}
        size="sm"
      />
      <span className="truncate">{agent ? row.source_agent_name : row.created_by_name}</span>
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
    <TableCell>
      <div className="flex items-center gap-1 tabular-nums">
        <span>{row.runs_7d}</span>
        {row.task && active && (
          <>
            <span aria-hidden="true" className="text-muted-foreground">·</span>
            <span className="text-primary" title={row.active_runs > 1 ? t(($) => $.wakeups.active_runs, { count: row.active_runs }) : undefined}>
              {text.runState(status)}
            </span>
          </>
        )}
        {row.task && (
          <span className={cn(!active && rowIntent)}>
            <TranscriptButton task={row.task} agentName={row.agent_name} title={ti(($) => $.wakeups.last_run)} isLive={active} />
          </span>
        )}
      </div>
    </TableCell>
  );
}

function SelectCell({ checked, visible, disabled, label, onChange }: {
  checked: boolean;
  visible: boolean;
  disabled: boolean;
  label: string;
  onChange: () => void;
}) {
  return (
    <TableCell className="w-8 pl-4 pr-0">
      <div className={cn(rowIntent, visible && "opacity-100")}>
        <Checkbox checked={checked} onCheckedChange={onChange} disabled={disabled} aria-label={label} />
      </div>
    </TableCell>
  );
}

/** The sub-issue system rule on one parent issue. */
function SystemWakeupListRow({ row, busy }: { row: WorkspaceWakeup; busy: boolean }) {
  const { t } = useT("autopilots");
  const { t: ti } = useT("issues");
  const wsId = useWorkspaceId();
  const update = useUpdateIssueSystemWakeup(wsId, row.issue_id);
  const title =
    row.system_stage != null
      ? ti(($) => $.wakeups.system.title_stage, { stage: row.system_stage })
      : ti(($) => $.wakeups.system.title_all);
  return (
    <TableRow className="group/row">
      <TableCell className="w-8 pl-4 pr-0" />
      <IssueCell row={row} />
      <TriggerCell icon={ListChecks} label={title} />
      {row.agent_id ? (
        <AgentCell id={row.agent_id} name={row.agent_name} label={t(($) => $.wakeups.assignee_target, { name: row.agent_name })} />
      ) : (
        <TableCell className="text-muted-foreground">{t(($) => $.wakeups.no_target)}</TableCell>
      )}
      <TableCell>
        <SourceCell row={row} />
      </TableCell>
      <TableCell className="text-muted-foreground">{t(($) => $.wakeups.until_issue_ends)}</TableCell>
      <RunsCell row={row} />
      <TableCell className="w-px py-0 pr-4">
        <div className="flex min-h-11 items-center justify-end px-2">
          <Switch
            checked={row.enabled}
            disabled={busy || update.isPending}
            aria-label={`${row.issue_identifier} · ${ti(($) => $.wakeups.system.toggle)}`}
            onCheckedChange={(enabled) =>
              update.mutate({ rule: "child_done", enabled }, { onError: () => toast.error(ti(($) => $.wakeups.system.save_error)) })
            }
          />
        </div>
      </TableCell>
    </TableRow>
  );
}

function WakeupListRow({
  row,
  selected,
  selecting,
  onSelect,
  busy,
}: {
  row: WorkspaceWakeup;
  selected: boolean;
  /** Some row is selected, so every checkbox stays visible. */
  selecting: boolean;
  onSelect: () => void;
  busy: boolean;
}) {
  const { t } = useT("autopilots");
  const { t: ti } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const locale = useLocale();
  const text = useWakeupText();
  const viewTZ = useViewingTimezone();
  const disable = useDisableIssueWakeup(wsId, row.issue_id);
  const enable = useEnableIssueWakeup(wsId, row.issue_id);
  const Icon = conditionIcon(row.condition) ?? (row.kind === "event" ? Bell : Clock3);
  // While enabled, a rule reads as when it ends; otherwise as its state.
  const ends = row.enabled && !row.issue_closed
    ? (text.ending(row) ?? (row.mode === "once" ? t(($) => $.wakeups.fires_once) : text.state(row)))
    : (text.paused(row) ?? text.state(row, row.issue_closed));
  return (
    <TableRow className="group/row" data-state={selected ? "selected" : undefined}>
      <SelectCell
        checked={selected}
        visible={selected || selecting}
        onChange={onSelect}
        disabled={busy || !row.can_manage || !row.enabled}
        label={t(($) => $.wakeups.select_row, { issue: row.issue_identifier, agent: row.agent_name })}
      />
      <IssueCell row={row} />
      <TriggerCell icon={Icon} label={text.trigger(row)} detail={row.kind === "cron" ? row.timezone : text.frequency(row)} />
      <AgentCell id={row.agent_id} name={row.agent_name} />
      <TableCell className="max-w-40">
        <SourceCell row={row} />
      </TableCell>
      <TableCell className="max-w-48">
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
            <AppLink href={paths.issueDetail(row.issue_id)} className="shrink-0 text-caption text-destructive">
              {ti(($) => $.wakeups.needs_attention)}
            </AppLink>
          )}
        </span>
      </TableCell>
      <RunsCell row={row} />
      {/* The controls are 44px targets, so this cell sets the row height. */}
      <TableCell className="w-px py-0 pr-4">
        <div
          className="flex items-center justify-end"
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
        </div>
      </TableCell>
    </TableRow>
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

function FilterSelect<T extends string>({
  label,
  value,
  items,
  disabled,
  onChange,
}: {
  label: string;
  value: T;
  items: { value: T; label: string }[];
  disabled: boolean;
  onChange: (value: T) => void;
}) {
  return (
    <Select
      items={items}
      value={value}
      disabled={disabled}
      onValueChange={(next) => {
        const item = items.find((candidate) => candidate.value === next);
        if (item) onChange(item.value);
      }}
    >
      <SelectTrigger size="sm" className="max-w-44" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function WorkspaceWakeups() {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const text = useWakeupText();
  const [filters, setFilters] = useState<WorkspaceWakeupFilters>({
    scope: "active",
    kind: "all",
    source: "",
    search: "",
    agent_id: "",
    offset: 0,
    limit: 50,
  });
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmation, setConfirmation] = useState<WorkspaceWakeup[]>([]);
  const [batchResult, setBatchResult] = useState<{
    failed: string[];
    succeeded: number;
  } | null>(null);
  const query = useQuery(workspaceWakeupsOptions(wsId, filters));
  // The newest rule the platform paused, for the banner above the list.
  const pausedQuery = useQuery({
    ...workspaceWakeupsOptions(wsId, { scope: "paused", kind: "all", source: "", search: "", agent_id: "", offset: 0, limit: 1 }),
    enabled: !!wsId && (query.data?.counts.paused ?? 0) > 0,
  });
  const batch = useDisableWorkspaceWakeups(wsId);
  const rows = query.data?.items ?? [];
  const selectable = rows.filter((row) => row.enabled && row.can_manage && row.source !== "system");
  const picked = selectable.filter((row) => selected.has(row.id));
  const change = useCallback((patch: Partial<WorkspaceWakeupFilters>) => {
    setFilters((prev) => ({ ...prev, offset: 0, ...patch }));
    setSelected(new Set());
    setBatchResult(null);
  }, []);
  // Search as you type, once typing pauses.
  useEffect(() => {
    const next = search.trim();
    if (next === filters.search) return;
    const timer = setTimeout(() => change({ search: next }), 300);
    return () => clearTimeout(timer);
  }, [search, filters.search, change]);
  const kinds = [
    { value: "all" as const, label: t(($) => $.wakeups.all_triggers) },
    { value: "event" as const, label: t(($) => $.wakeups.event) },
    { value: "at" as const, label: t(($) => $.wakeups.at) },
    { value: "recurring" as const, label: t(($) => $.wakeups.recurring) },
  ];
  const agents = [
    { value: "", label: t(($) => $.wakeups.all_agents) },
    ...(query.data?.agents ?? []).map((a) => ({ value: a.id, label: a.name })),
  ];
  const sources = [
    { value: "" as const, label: t(($) => $.wakeups.sources.all) },
    { value: "member" as const, label: t(($) => $.wakeups.sources.member) },
    { value: "agent" as const, label: t(($) => $.wakeups.sources.agent) },
    { value: "system" as const, label: t(($) => $.wakeups.sources.system) },
  ];
  const pausedCount = query.data?.counts.paused ?? 0;
  const latestPaused = pausedQuery.data?.items[0];
  const filtered = filters.scope !== "all" || !!filters.search || filters.kind !== "all" || !!filters.source || !!filters.agent_id;
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
    const hasAny = (query.data?.counts.all ?? 0) > 0;
    body = (
      <CollectionPageState
        icon={Bell}
        title={t(($) => (hasAny || filtered ? $.wakeups.empty_filtered : $.wakeups.empty))}
        actions={
          filtered && hasAny ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setSearch("");
                change({ scope: "all", kind: "all", source: "", search: "", agent_id: "" });
              }}
            >
              {t(($) => $.wakeups.clear_filters)}
            </Button>
          ) : undefined
        }
      />
    );
  } else {
    const selecting = picked.length > 0;
    body = (
      <div className="min-h-0 flex-1 overflow-auto">
        <Table className="min-w-[900px]">
          <TableHeader>
            <TableRow className="group/row">
              <TableHead className="w-8 pl-4 pr-0">
                <div className={cn(rowIntent, selecting && "opacity-100")}>
                  <Checkbox
                    disabled={!selectable.length || batch.isPending}
                    checked={picked.length > 0 && picked.length === selectable.length}
                    indeterminate={picked.length > 0 && picked.length < selectable.length}
                    onCheckedChange={() =>
                      setSelected(picked.length === selectable.length ? new Set() : new Set(selectable.map((row) => row.id)))
                    }
                    aria-label={t(($) => $.wakeups.select_page)}
                  />
                </div>
              </TableHead>
              <TableHead>{t(($) => $.wakeups.issue)}</TableHead>
              <TableHead>{t(($) => $.wakeups.trigger)}</TableHead>
              <TableHead>{t(($) => $.wakeups.target_agent)}</TableHead>
              <TableHead>{t(($) => $.wakeups.source)}</TableHead>
              <TableHead>{t(($) => $.wakeups.ends)}</TableHead>
              <TableHead>{t(($) => $.wakeups.runs_7d)}</TableHead>
              <TableHead className="w-px pr-6 text-right">{t(($) => $.wakeups.enabled)}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) =>
              row.source === "system" ? (
                <SystemWakeupListRow key={`system-${row.id}`} row={row} busy={batch.isPending} />
              ) : (
                <WakeupListRow
                  key={row.id}
                  row={row}
                  busy={batch.isPending}
                  selected={selected.has(row.id)}
                  selecting={selecting}
                  onSelect={() =>
                    setSelected((prev) => {
                      const next = new Set(prev);
                      if (next.has(row.id)) next.delete(row.id);
                      else next.add(row.id);
                      return next;
                    })
                  }
                />
              ),
            )}
          </TableBody>
        </Table>
      </div>
    );
  }
  return (
    <>
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
      {/* One row: scope on the left, the narrowing filters and search on the
          right. It only wraps on narrow windows. */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 px-4 pb-2 pt-3">
        <div role="group" aria-label={t(($) => $.wakeups.scope)} className="flex shrink-0 items-center gap-0.5 rounded-lg bg-muted p-0.5">
          {(["active", "paused", "disabled", "ended", "all"] as const).map((scope) => {
            const active = filters.scope === scope;
            return (
              <button
                key={scope}
                type="button"
                aria-pressed={active}
                disabled={batch.isPending}
                onClick={() => change({ scope })}
                className={cn(
                  "inline-flex h-7 items-center gap-1 rounded-md px-2.5 text-caption text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50",
                  active && "bg-surface font-medium text-foreground shadow-[var(--surface-shadow)]",
                )}
              >
                {t(($) => $.wakeups.scopes[scope])}
                <span className="tabular-nums text-muted-foreground">{query.data?.counts[scope] ?? "—"}</span>
              </button>
            );
          })}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <FilterSelect label={t(($) => $.wakeups.source)} value={filters.source} items={sources} disabled={batch.isPending} onChange={(source) => change({ source })} />
          <FilterSelect label={t(($) => $.wakeups.trigger)} value={filters.kind} items={kinds} disabled={batch.isPending} onChange={(kind) => change({ kind })} />
          <FilterSelect label={t(($) => $.wakeups.target_agent)} value={filters.agent_id} items={agents} disabled={batch.isPending} onChange={(agent_id) => change({ agent_id })} />
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              type="search"
              className="h-8 w-52 pl-8"
              value={search}
              maxLength={256}
              disabled={batch.isPending}
              onChange={(event) => setSearch(event.target.value)}
              aria-label={t(($) => $.wakeups.search)}
              placeholder={t(($) => $.wakeups.search)}
            />
          </div>
        </div>
      </div>
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
      {body}
      <div className="mt-auto flex shrink-0 flex-wrap items-center gap-2 border-t px-4 py-2 text-caption text-muted-foreground">
        {picked.length > 0 && (
          <>
            <span>{t(($) => $.wakeups.selected, { count: picked.length })}</span>
            <Button size="sm" variant="outline" disabled={batch.isPending} onClick={() => setConfirmation(picked)}>
              {t(($) => $.wakeups.disable_selected)}
            </Button>
            <Button size="sm" variant="ghost" disabled={batch.isPending} onClick={() => setSelected(new Set())}>
              {t(($) => $.wakeups.clear)}
            </Button>
          </>
        )}
        <span className="ml-auto tabular-nums">
          {query.data && !query.isError
            ? t(($) => $.wakeups.results, {
                count: query.data?.total ?? 0,
                page: Math.floor(filters.offset / filters.limit) + 1,
              })
            : "—"}
        </span>
        <Button
          size="sm"
          variant="ghost"
          disabled={batch.isPending || !filters.offset}
          onClick={() => change({ offset: Math.max(0, filters.offset - filters.limit) })}
        >
          {t(($) => $.wakeups.previous)}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={batch.isPending || !query.data || filters.offset + filters.limit >= query.data.total}
          onClick={() => change({ offset: filters.offset + filters.limit })}
        >
          {t(($) => $.wakeups.next_page)}
        </Button>
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
    </>
  );
}

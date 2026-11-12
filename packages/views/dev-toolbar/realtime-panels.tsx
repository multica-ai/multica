"use client";

import { useState } from "react";
import { useQuery, useQueryClient, type Query } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  clearDevEvents,
  dropDevConnection,
  replayDevEvent,
  type DevCacheChange,
  type DevRealtimeEvent,
  type DevRecording,
} from "@multica/core/diagnostics";
import { agentListOptions } from "@multica/core/workspace/queries";
import { readRuntimeCliVersion, runtimeDisplayName } from "@multica/core/runtimes";
import type { AgentRuntime, AgentTask } from "@multica/core/types";
import { ChevronDown, ChevronRight, Copy, ExternalLink, RefreshCw, RotateCcw, Trash2, Unplug } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  activeTasks,
  CACHE_CHANGE_LABEL,
  cacheEffectSummary,
  eventMatches,
  eventsInLastMinute,
  eventSubject,
  formatAge,
  formatClock,
  formatDuration,
  issueIdsForKey,
  taskStartedAt,
} from "./metrics";
import { copy, PanelHeader, Rows, SmallButton, Stats } from "./panels";

const FIELD_CLASS =
  "h-6 min-w-0 rounded-md border border-border bg-background px-2 text-caption text-foreground outline-none placeholder:text-muted-foreground focus:border-ring";
const SHOWN_MAX = 200;
const LIVE = "live";

function KeyList({ change, keys }: { change: DevCacheChange; keys: readonly string[] }) {
  if (keys.length === 0) return null;
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground">{`${CACHE_CHANGE_LABEL[change]} (${keys.length})`}</span>
      <div className="max-h-24 overflow-auto rounded-md bg-muted px-2 py-1 font-mono text-micro">
        {keys.map((key) => (
          <div key={key} className="truncate" title={key}>
            {key}
          </div>
        ))}
      </div>
    </div>
  );
}

function ReconnectRow({ event }: { event: DevRealtimeEvent }) {
  const { invalidated, refetched } = event.cache;
  return (
    <div className="flex items-center gap-2 border-y border-warning/30 bg-warning/10 px-3 py-1 font-mono">
      <Unplug className="size-3 shrink-0 text-warning" />
      <span className="shrink-0 text-muted-foreground">{formatClock(event.at)}</span>
      <span className="min-w-0 flex-1 truncate text-foreground">
        {event.downMs === null ? "Reconnected" : `Reconnected after ${formatDuration(event.downMs)} down`}
      </span>
      <span className="shrink-0 text-muted-foreground">
        {`refetched ${refetched.length} · invalidated ${invalidated.length}`}
      </span>
    </div>
  );
}

function EventRow({
  event,
  expanded,
  onToggle,
  onOpenIssue,
}: {
  event: DevRealtimeEvent;
  expanded: boolean;
  onToggle: () => void;
  onOpenIssue: ((issueId: string) => void) | null;
}) {
  const summary = cacheEffectSummary(event.cache);
  const Chevron = expanded ? ChevronDown : ChevronRight;
  const replay = () => {
    const refusal = replayDevEvent(event.id);
    if (refusal) toast.error(`Cannot replay: ${refusal}`);
    else toast.success(`Replayed ${event.type} locally`);
  };
  return (
    <div className={cn(event.kind === "replay" && "bg-brand/5")}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="flex w-full items-center gap-2 px-3 py-1 text-left font-mono hover:bg-accent"
      >
        <Chevron className="size-3 shrink-0 text-muted-foreground" />
        <span className="shrink-0 text-muted-foreground">{formatClock(event.at)}</span>
        <span className="shrink-0 text-foreground">{event.type}</span>
        {event.kind === "replay" && <span className="shrink-0 text-brand">replay</span>}
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{eventSubject(event)}</span>
        <span className={cn("shrink-0", summary ? "text-foreground" : "text-muted-foreground")}>
          {summary || "no cache change"}
        </span>
      </button>
      {expanded && (
        <div className="flex flex-col gap-2 px-3 pt-1 pb-2 pl-8">
          {event.actor && <div className="font-mono text-muted-foreground">{`by ${event.actor}`}</div>}
          {summary ? (
            (Object.keys(CACHE_CHANGE_LABEL) as DevCacheChange[]).map((change) => (
              <KeyList key={change} change={change} keys={event.cache[change]} />
            ))
          ) : (
            <div className="text-muted-foreground">
              Handling this event did not touch the query cache, or did so only after an await.
            </div>
          )}
          {event.payload !== null && (
            <pre className="max-h-40 overflow-auto rounded-md bg-muted p-2 font-mono text-micro whitespace-pre-wrap break-all">
              {event.payload}
            </pre>
          )}
          {event.payloadNote && <div className="text-muted-foreground">{`Payload ${event.payloadNote}.`}</div>}
          <div className="flex gap-1">
            <SmallButton onClick={replay}>
              <RotateCcw className="size-3" />
              Replay locally
            </SmallButton>
            {event.payload !== null && (
              <SmallButton onClick={() => void copy(event.payload!, "payload")}>
                <Copy className="size-3" />
                Copy payload
              </SmallButton>
            )}
            {event.issueId && onOpenIssue && (
              <SmallButton onClick={() => onOpenIssue(event.issueId!)}>
                <ExternalLink className="size-3" />
                Open issue
              </SmallButton>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export function EventsPanel({
  recording,
  now,
  onOpenIssue,
}: {
  recording: DevRecording;
  now: number;
  onOpenIssue: ((issueId: string) => void) | null;
}) {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [type, setType] = useState("");
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const { events } = recording;

  const typeCounts = new Map<string, number>();
  for (const event of events) {
    if (event.kind !== "reconnect") typeCounts.set(event.type, (typeCounts.get(event.type) ?? 0) + 1);
  }
  const issueIds = issueIdsForKey(
    search,
    events,
    queryClient.getQueryCache().getAll().map((q) => q.state.data),
  );
  const shown = events
    .filter((e) => (type ? e.type === type : true) && eventMatches(e, search, issueIds))
    .slice(-SHOWN_MAX)
    .reverse();
  const reconnects = events.filter((e) => e.kind === "reconnect").length;
  const changed = events.filter((e) => e.kind !== "reconnect" && cacheEffectSummary(e.cache)).length;
  const drop = () => {
    if (!dropDevConnection()) toast.error("No live connection to drop");
  };

  return (
    <>
      <PanelHeader title="Realtime events">
        <SmallButton onClick={drop}>
          <Unplug className="size-3" />
          Drop connection
        </SmallButton>
        <SmallButton onClick={clearDevEvents}>
          <Trash2 className="size-3" />
          Clear
        </SmallButton>
      </PanelHeader>
      <Stats
        items={[
          { label: "Last minute", value: String(eventsInLastMinute(recording.frameCounts, now)) },
          { label: "Recorded", value: String(events.length - reconnects) },
          { label: "Changed cache", value: String(changed) },
          { label: "Reconnects", value: String(reconnects), tone: reconnects > 0 ? "warn" : "normal" },
        ]}
      />
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <input
          aria-label="Search events"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Issue key, ID or payload text"
          className={cn(FIELD_CLASS, "flex-1")}
        />
        <select
          aria-label="Event type"
          value={type}
          onChange={(e) => setType(e.target.value)}
          className={cn(FIELD_CLASS, "w-48")}
        >
          <option value="">All types</option>
          {[...typeCounts].sort(([a], [b]) => a.localeCompare(b)).map(([name, count]) => (
            <option key={name} value={name}>{`${name} (${count})`}</option>
          ))}
        </select>
      </div>
      <Rows empty={events.length === 0 ? "No realtime events yet." : "No events match."}>
        {shown.map((event) =>
          event.kind === "reconnect" ? (
            <ReconnectRow key={event.id} event={event} />
          ) : (
            <EventRow
              key={event.id}
              event={event}
              expanded={expandedId === event.id}
              onToggle={() => setExpandedId((id) => (id === event.id ? null : event.id))}
              onOpenIssue={onOpenIssue}
            />
          ),
        )}
      </Rows>
    </>
  );
}

type CacheFilter = "all" | "stale" | "fetching";

function queryState(query: Query): { label: string; className: string } {
  if (query.state.fetchStatus === "fetching") return { label: "fetching", className: "text-brand" };
  if (query.state.status === "error") return { label: "error", className: "text-destructive" };
  if (query.state.status === "pending") return { label: "empty", className: "text-muted-foreground" };
  if (query.isStale()) return { label: "stale", className: "text-warning" };
  return { label: "fresh", className: "text-foreground" };
}

export function CachePanel({ now }: { now: number }) {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<CacheFilter>("all");
  const queries = queryClient.getQueryCache().getAll();
  const stale = queries.filter((q) => q.isStale());
  const fetching = queries.filter((q) => q.state.fetchStatus === "fetching");
  const needle = search.trim().toLowerCase();
  const shown = (filter === "stale" ? stale : filter === "fetching" ? fetching : queries)
    .filter((q) => !needle || q.queryHash.toLowerCase().includes(needle))
    .sort((a, b) => b.state.dataUpdatedAt - a.state.dataUpdatedAt)
    .slice(0, SHOWN_MAX);
  const invalidate = (query: Query) => {
    void queryClient.invalidateQueries({ queryKey: query.queryKey, exact: true });
  };

  return (
    <>
      <PanelHeader title="Query cache" note={LIVE}>
        {(["all", "stale", "fetching"] as const).map((value) => (
          <SmallButton key={value} onClick={() => setFilter(value)} active={filter === value}>
            {value[0]!.toUpperCase() + value.slice(1)}
          </SmallButton>
        ))}
      </PanelHeader>
      <Stats
        items={[
          { label: "Queries", value: String(queries.length) },
          { label: "Stale", value: String(stale.length), tone: stale.length > 0 ? "warn" : "normal" },
          { label: "Fetching", value: String(fetching.length) },
          { label: "Unobserved", value: String(queries.filter((q) => q.getObserversCount() === 0).length) },
        ]}
      />
      <div className="flex items-center border-b border-border px-3 py-2">
        <input
          aria-label="Search query keys"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search query keys"
          className={cn(FIELD_CLASS, "flex-1")}
        />
      </div>
      <Rows empty={queries.length === 0 ? "The cache is empty." : "No queries match."}>
        {shown.map((query) => {
          const state = queryState(query);
          const observers = query.getObserversCount();
          return (
            <div key={query.queryHash} className="flex items-center gap-2 px-3 py-0.5 font-mono">
              <span className="min-w-0 flex-1 truncate" title={query.queryHash}>
                {query.queryHash}
              </span>
              <span className={cn("w-14 shrink-0 text-right", state.className)}>{state.label}</span>
              <span className="w-8 shrink-0 text-right text-muted-foreground" title="Observers">
                {observers === 0 ? "–" : `×${observers}`}
              </span>
              <span className="w-8 shrink-0 text-right text-muted-foreground" title="Last updated">
                {query.state.dataUpdatedAt ? formatAge(now - query.state.dataUpdatedAt) : "—"}
              </span>
              <button
                type="button"
                onClick={() => invalidate(query)}
                aria-label={`Invalidate ${query.queryHash}`}
                title="Invalidate"
                className="flex size-5 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <RefreshCw className="size-3" />
              </button>
            </div>
          );
        })}
      </Rows>
    </>
  );
}

export function RuntimesPanel({ runtimes, now }: { runtimes: readonly AgentRuntime[] | undefined; now: number }) {
  const list = runtimes ?? [];
  const online = list.filter((r) => r.status === "online");
  const versions = [...new Set(list.map((r) => readRuntimeCliVersion(r.metadata)).filter(Boolean))];
  return (
    <>
      <PanelHeader title="Runtimes" note={LIVE} />
      <Stats
        items={[
          { label: "Online", value: String(online.length), tone: list.length > 0 && online.length === 0 ? "bad" : "normal" },
          { label: "Offline", value: String(list.length - online.length) },
          { label: "CLI versions", value: versions.join(", ") || "—" },
        ]}
      />
      <Rows empty={runtimes === undefined ? "Loading runtimes…" : "No runtimes in this workspace."}>
        {list.map((runtime) => {
          const version = readRuntimeCliVersion(runtime.metadata);
          const seen = runtime.last_seen_at ? Date.parse(runtime.last_seen_at) : null;
          return (
            <div key={runtime.id} className="flex items-center gap-2 px-3 py-1 font-mono">
              <span
                className={cn(
                  "size-1.5 shrink-0 rounded-full",
                  runtime.status === "online" ? "bg-success" : "bg-muted-foreground/50",
                )}
              />
              <span className="min-w-0 flex-1 truncate text-foreground" title={runtime.device_info}>
                {runtimeDisplayName(runtime)}
              </span>
              <span className="shrink-0 text-muted-foreground">{`${runtime.provider} · ${runtime.runtime_mode}`}</span>
              <span className="max-w-44 shrink-0 truncate text-right" title={version ?? undefined}>
                {version ? `v${version.replace(/^v/, "")}` : "—"}
              </span>
              <span className="w-14 shrink-0 text-right text-muted-foreground" title="Last heartbeat">
                {seen === null ? "—" : formatAge(now - seen)}
              </span>
            </div>
          );
        })}
      </Rows>
    </>
  );
}

function taskSource(task: AgentTask): string {
  if (task.issue_id) return task.issue_identifier ?? task.issue_id.slice(0, 8);
  if (task.chat_session_id) return "chat";
  if (task.autopilot_run_id) return "autopilot";
  return "—";
}

export function TasksPanel({
  tasks,
  wsId,
  now,
  onOpenIssue,
}: {
  tasks: readonly AgentTask[] | undefined;
  wsId: string;
  now: number;
  onOpenIssue: ((issueId: string) => void) | null;
}) {
  const { data: agents } = useQuery(agentListOptions(wsId));
  const agentName = new Map(agents?.map((a) => [a.id, a.name]));
  const active = activeTasks(tasks ?? []);
  const running = active.filter((t) => t.status === "running").length;
  return (
    <>
      <PanelHeader title="Agent tasks" note={LIVE} />
      <Stats
        items={[
          { label: "Running", value: String(running) },
          { label: "Queued", value: String(active.filter((t) => t.status === "queued" || t.status === "dispatched").length) },
          { label: "Waiting", value: String(active.filter((t) => t.status === "waiting_local_directory" || t.status === "deferred").length) },
        ]}
      />
      <Rows empty={tasks === undefined ? "Loading tasks…" : "No agent task is queued or running."}>
        {active.map((task) => {
          const open = task.issue_id && onOpenIssue ? () => onOpenIssue(task.issue_id) : undefined;
          return (
            <button
              type="button"
              key={task.id}
              onClick={open}
              disabled={!open}
              title={open ? "Open the issue" : undefined}
              className="flex w-full items-center gap-2 px-3 py-1 text-left font-mono enabled:hover:bg-accent"
            >
              <span className={cn("w-20 shrink-0", task.status === "running" ? "text-success" : "text-warning")}>
                {task.status === "waiting_local_directory" ? "waiting" : task.status}
              </span>
              <span className="min-w-0 flex-1 truncate text-foreground">
                {agentName.get(task.agent_id) ?? task.agent_id.slice(0, 8)}
              </span>
              <span className="min-w-0 max-w-40 shrink-0 truncate text-muted-foreground" title={task.issue_title}>
                {taskSource(task)}
              </span>
              <span className="w-12 shrink-0 text-right tabular-nums text-muted-foreground">
                {formatAge(now - taskStartedAt(task))}
              </span>
            </button>
          );
        })}
      </Rows>
    </>
  );
}

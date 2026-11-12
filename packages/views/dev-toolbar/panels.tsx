"use client";

import { useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  clearDevErrors,
  clearDevRequests,
  DEV_RECORDING_WINDOW_MS,
  type DevRecording,
  type DevRequest,
} from "@multica/core/diagnostics";
import { ChevronDown, ChevronRight, Copy, Trash2 } from "@multica/ui/icons";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import {
  curlCommand,
  firstStackFrame,
  formatBytes,
  formatClock,
  formatDuration,
  memoryTone,
  navTone,
  percentile,
  requestFailed,
  type MetricTone,
} from "./metrics";

export const TONE_CLASS: Record<MetricTone, string> = {
  normal: "text-foreground",
  warn: "text-warning",
  bad: "text-destructive",
};

function statusTone(request: DevRequest): MetricTone {
  if (requestFailed(request)) return "bad";
  if (request.durationMs !== null && request.durationMs >= 1000) return "warn";
  return "normal";
}

function longTaskTone(ms: number): MetricTone {
  if (ms >= 200) return "bad";
  if (ms >= 100) return "warn";
  return "normal";
}

async function copy(text: string, what: string) {
  if (await copyText(text)) toast.success(`Copied ${what}`);
  else toast.error(`Could not copy ${what}`);
}

function PanelHeader({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="flex items-center gap-2 border-b border-border px-3 py-2">
      <span className="font-medium text-foreground">{title}</span>
      <span className="text-micro text-muted-foreground">last {DEV_RECORDING_WINDOW_MS / 60_000} min</span>
      <div className="ml-auto flex items-center gap-1">{children}</div>
    </div>
  );
}

function Stats({ items }: { items: { label: string; value: string; tone?: MetricTone }[] }) {
  return (
    <div className="grid grid-flow-col auto-cols-fr border-b border-border">
      {items.map((item) => (
        <div key={item.label} className="flex flex-col gap-0.5 px-3 py-2">
          <span className="text-micro text-muted-foreground">{item.label}</span>
          <span className={cn("font-mono tabular-nums", TONE_CLASS[item.tone ?? "normal"])}>{item.value}</span>
        </div>
      ))}
    </div>
  );
}

function Rows({ children, empty }: { children: ReactNode[]; empty: string }) {
  if (children.length === 0) {
    return <div className="px-3 py-6 text-center text-muted-foreground">{empty}</div>;
  }
  return <div className="max-h-80 overflow-y-auto py-1">{children}</div>;
}

function SmallButton({
  onClick,
  children,
  active,
  disabled,
}: {
  onClick: () => void;
  children: ReactNode;
  active?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex h-6 items-center gap-1 rounded-md px-1.5 text-micro text-muted-foreground hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50",
        active && "bg-accent text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function Sparkline({ samples }: { samples: DevRecording["memory"] }) {
  if (samples.length < 2) {
    return <div className="grid h-16 place-items-center text-muted-foreground">Collecting samples…</div>;
  }
  const width = 400;
  const height = 64;
  const first = samples[0]!.at;
  const span = Math.max(1, samples.at(-1)!.at - first);
  const values = samples.map((s) => s.bytes);
  const min = Math.min(...values);
  const range = Math.max(1, Math.max(...values) - min);
  const points = samples
    .map((s) => `${((s.at - first) / span) * width},${height - 4 - ((s.bytes - min) / range) * (height - 8)}`)
    .join(" ");
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="h-16 w-full text-brand">
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

export function MemoryPanel({ recording }: { recording: DevRecording }) {
  const queries = useQueryClient().getQueryCache().getAll();
  const samples = recording.memory;
  const current = samples.at(-1)?.bytes ?? null;
  const peak = samples.length ? Math.max(...samples.map((s) => s.bytes)) : null;
  return (
    <>
      <PanelHeader title="Memory" />
      <Stats
        items={[
          { label: "JS heap", value: current === null ? "—" : formatBytes(current), tone: current === null ? "normal" : memoryTone(current) },
          { label: "Peak", value: peak === null ? "—" : formatBytes(peak) },
          { label: "Limit", value: recording.heapLimit === null ? "—" : formatBytes(recording.heapLimit) },
        ]}
      />
      <div className="border-b border-border px-3 py-2">
        {current === null ? (
          <div className="py-4 text-center text-muted-foreground">This engine does not report heap size.</div>
        ) : (
          <Sparkline samples={samples} />
        )}
      </div>
      <Stats
        items={[
          { label: "Cached queries", value: String(queries.length) },
          { label: "Stale", value: String(queries.filter((q) => q.isStale()).length) },
          { label: "Fetching", value: String(queries.filter((q) => q.state.fetchStatus === "fetching").length) },
        ]}
      />
    </>
  );
}

export function NavigationPanel({ recording }: { recording: DevRecording }) {
  const visits = recording.navigations.slice(-20).reverse();
  const timed = visits.filter((v) => v.durationMs !== null);
  const slowest = timed.reduce<(typeof timed)[number] | null>(
    (max, v) => (max === null || v.durationMs! > max.durationMs! ? v : max),
    null,
  );
  const median = percentile(timed.map((v) => v.durationMs!), 50);
  return (
    <>
      <PanelHeader title="Navigation" />
      <Stats
        items={[
          { label: "Route changes", value: String(recording.navigations.length) },
          { label: "Median", value: median === null ? "—" : formatDuration(median), tone: median === null ? "normal" : navTone(median) },
          {
            label: "Slowest",
            value: slowest ? formatDuration(slowest.durationMs!) : "—",
            tone: slowest ? navTone(slowest.durationMs!) : "normal",
          },
        ]}
      />
      <Rows empty="No route changes yet.">
        {visits.map((visit) => (
          <div
            key={`${visit.at}-${visit.route}`}
            className={cn("flex items-center gap-3 px-3 py-1 font-mono", visit === slowest && "bg-warning/10")}
          >
            <span className="shrink-0 text-muted-foreground">{formatClock(visit.at)}</span>
            <span className="min-w-0 flex-1 truncate">{visit.route}</span>
            <span
              className={cn(
                "shrink-0 tabular-nums",
                visit.durationMs === null ? "text-muted-foreground" : TONE_CLASS[navTone(visit.durationMs)],
              )}
            >
              {visit.durationMs === null ? "—" : formatDuration(visit.durationMs)}
            </span>
          </div>
        ))}
      </Rows>
    </>
  );
}

export function LongTaskPanel({ recording, blocked }: { recording: DevRecording; blocked: number | null }) {
  const tasks = [...recording.longTasks].reverse();
  const total = tasks.reduce((sum, task) => sum + task.durationMs, 0);
  return (
    <>
      <PanelHeader title="Long tasks" />
      <Stats
        items={[
          { label: "Blocked, last 10s", value: blocked === null ? "—" : `${Math.round(blocked * 100)}%` },
          { label: "Long tasks", value: String(tasks.length) },
          { label: "Total blocked", value: formatDuration(total) },
        ]}
      />
      <Rows empty={blocked === null ? "This engine does not report long tasks." : "No task has blocked the main thread for 50ms or more."}>
        {tasks.map((task) => (
          <div key={task.at} className="flex items-center gap-3 px-3 py-1 font-mono">
            <span className="shrink-0 text-muted-foreground">{formatClock(task.at)}</span>
            <span className="min-w-0 flex-1 truncate">{task.route ?? "—"}</span>
            <span className={cn("shrink-0 tabular-nums", TONE_CLASS[longTaskTone(task.durationMs)])}>
              {formatDuration(task.durationMs)}
            </span>
          </div>
        ))}
      </Rows>
    </>
  );
}

function RequestRow({
  request,
  apiUrl,
  workspaceSlug,
  expanded,
  onToggle,
}: {
  request: DevRequest;
  apiUrl: string;
  workspaceSlug: string | null;
  expanded: boolean;
  onToggle: () => void;
}) {
  const tone = statusTone(request);
  const curl = expanded ? curlCommand(apiUrl, request, workspaceSlug) : null;
  const Chevron = expanded ? ChevronDown : ChevronRight;
  return (
    <div className={cn(requestFailed(request) && "bg-destructive/5")}>
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2 px-3 py-1 text-left font-mono hover:bg-accent"
      >
        <Chevron className="size-3 shrink-0 text-muted-foreground" />
        <span className="shrink-0 text-muted-foreground">{formatClock(request.startedAt)}</span>
        <span className="w-11 shrink-0">{request.method}</span>
        <span className="min-w-0 flex-1 truncate" title={request.path}>
          {request.path}
        </span>
        <span
          className={cn(
            "w-14 shrink-0 text-right tabular-nums",
            request.error === "aborted" ? "text-muted-foreground" : TONE_CLASS[tone],
          )}
        >
          {request.durationMs === null
            ? "…"
            : (request.status ?? (request.error === "aborted" ? "canceled" : "ERR"))}
        </span>
        <span className="w-14 shrink-0 text-right tabular-nums text-muted-foreground">
          {request.durationMs === null ? "" : formatDuration(request.durationMs)}
        </span>
      </button>
      {expanded && (
        <div className="flex flex-col gap-2 px-3 pt-1 pb-2 pl-8">
          <div className="font-mono break-all">{request.path}</div>
          {request.error && <div className="break-words text-destructive">{request.error}</div>}
          <div className="font-mono text-muted-foreground">X-Request-ID {request.requestId}</div>
          {request.body !== null && (
            <pre className="max-h-32 overflow-auto rounded-md bg-muted p-2 font-mono text-micro whitespace-pre-wrap break-all">
              {request.body}
            </pre>
          )}
          {request.bodyNote && (
            <div className="text-muted-foreground">
              {`Body ${request.bodyNote}. Copy as curl is off because the command would not send the same request.`}
            </div>
          )}
          <div className="flex gap-1">
            <SmallButton onClick={() => curl && void copy(curl, "curl")} disabled={curl === null}>
              <Copy className="size-3" />
              Copy as curl
            </SmallButton>
            <SmallButton onClick={() => void copy(request.requestId, "request ID")}>
              <Copy className="size-3" />
              Copy request ID
            </SmallButton>
          </div>
        </div>
      )}
    </div>
  );
}

export function NetworkPanel({
  recording,
  apiUrl,
  workspaceSlug,
}: {
  recording: DevRecording;
  apiUrl: string;
  workspaceSlug: string | null;
}) {
  const [failedOnly, setFailedOnly] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const requests = recording.requests;
  const failed = requests.filter(requestFailed);
  const durations = requests.flatMap((r) => (r.durationMs === null ? [] : [r.durationMs]));
  const p95 = percentile(durations, 95);
  const shown = (failedOnly ? failed : requests).slice(-100).reverse();
  return (
    <>
      <PanelHeader title="Network">
        <SmallButton onClick={() => setFailedOnly((v) => !v)} active={failedOnly}>
          Failed only
        </SmallButton>
        <SmallButton onClick={clearDevRequests}>
          <Trash2 className="size-3" />
          Clear
        </SmallButton>
      </PanelHeader>
      <Stats
        items={[
          { label: "Requests", value: String(requests.length) },
          { label: "In flight", value: String(requests.filter((r) => r.durationMs === null).length) },
          { label: "Failed", value: String(failed.length), tone: failed.length > 0 ? "bad" : "normal" },
          { label: "p95", value: p95 === null ? "—" : formatDuration(p95), tone: p95 !== null && p95 >= 1000 ? "warn" : "normal" },
        ]}
      />
      <Rows empty={failedOnly ? "No failed requests." : "No API requests yet."}>
        {shown.map((request) => (
          <RequestRow
            key={request.requestId}
            request={request}
            apiUrl={apiUrl}
            workspaceSlug={workspaceSlug}
            expanded={expandedId === request.requestId}
            onToggle={() => setExpandedId((id) => (id === request.requestId ? null : request.requestId))}
          />
        ))}
      </Rows>
    </>
  );
}

export function SyncPanel({ recording, status }: { recording: DevRecording; status: string }) {
  const changes = recording.sync;
  const reconnects = changes.filter((c) => c.status === "reconnecting").length;
  return (
    <>
      <PanelHeader title="Realtime" />
      <Stats
        items={[
          { label: "Status", value: status, tone: status === "reconnecting" ? "bad" : status === "connecting" ? "warn" : "normal" },
          { label: "Reconnects", value: String(reconnects), tone: reconnects > 0 ? "warn" : "normal" },
        ]}
      />
      <Rows empty="No connection changes yet.">
        {[...changes].reverse().map((change, i, list) => {
          const next = i === 0 ? null : list[i - 1]!;
          return (
            <div key={change.at} className="flex items-center gap-3 px-3 py-1 font-mono">
              <span className="shrink-0 text-muted-foreground">{formatClock(change.at)}</span>
              <span className="min-w-0 flex-1">{change.status}</span>
              <span className="shrink-0 tabular-nums text-muted-foreground">
                {next ? `for ${formatDuration(next.at - change.at)}` : "now"}
              </span>
            </div>
          );
        })}
      </Rows>
    </>
  );
}

export function ErrorsPanel({ recording }: { recording: DevRecording }) {
  const [expanded, setExpanded] = useState<number | null>(null);
  const errors = [...recording.errors].reverse();
  return (
    <>
      <PanelHeader title="Errors">
        <SmallButton onClick={clearDevErrors}>
          <Trash2 className="size-3" />
          Clear
        </SmallButton>
      </PanelHeader>
      <Rows empty="No errors. Console errors, uncaught exceptions and unhandled rejections show up here.">
        {errors.map((error) => {
          const frame = firstStackFrame(error.stack);
          const open = expanded === error.at;
          return (
            <button
              type="button"
              key={`${error.at}-${error.message}`}
              onClick={() => setExpanded(open ? null : error.at)}
              className="flex w-full flex-col gap-0.5 border-b border-border/50 px-3 py-1.5 text-left last:border-b-0 hover:bg-accent"
            >
              <span className="flex items-center gap-2 font-mono text-muted-foreground">
                <span>{formatClock(error.at)}</span>
                <span>{error.source}</span>
                <span className="min-w-0 truncate">{error.route}</span>
              </span>
              <span className={cn("break-words text-destructive", !open && "line-clamp-2")}>{error.message}</span>
              {open && error.stack ? (
                <pre className="max-h-40 overflow-auto font-mono text-micro whitespace-pre-wrap text-muted-foreground">
                  {error.stack}
                </pre>
              ) : (
                frame && <span className="truncate font-mono text-micro text-muted-foreground">{frame}</span>
              )}
            </button>
          );
        })}
      </Rows>
    </>
  );
}

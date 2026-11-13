"use client";

import { useState, useSyncExternalStore, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import {
  getCurrentSlug,
  getCurrentWsId,
  subscribeToCurrentSlug,
} from "@multica/core/platform";
import { useWSStatus, type WSStatus } from "@multica/core/realtime";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { ChevronUp, Minus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  activeTasks,
  apiHost,
  eventsInLastMinute,
  formatBytes,
  jankTone,
  memoryTone,
  navTone,
  requestFailed,
  type MetricTone,
} from "./metrics";
import {
  ErrorsPanel,
  LongTaskPanel,
  MemoryPanel,
  NavigationPanel,
  NetworkPanel,
  SyncPanel,
  TONE_CLASS,
} from "./panels";
import { CachePanel, EventsPanel, RuntimesPanel, TasksPanel } from "./realtime-panels";
import { useDevRecorder, useMainThreadBlocked, useNavDuration, useNow } from "./use-dev-metrics";

export interface DevToolbarProps {
  /** Which client this is, e.g. `web` or `desktop · staging`. */
  build: string;
  /** Backend the client talks to. */
  apiUrl: string;
  /** Current route as the platform's router sees it. */
  route: string;
  /** Open an in-app path, such as the issue an agent task runs for. */
  onNavigate: (path: string) => void;
}

// App shells size themselves to the viewport (`h-svh`, `h-screen`), so a fixed
// bar would cover their bottom edge — the sidebar's account menu, composers.
// While the bar is shown these utilities give up its height, so the app ends
// where the bar begins. Unlayered, this beats Tailwind's layered utilities.
const BAR_HEIGHT_PX = 24;
const RESERVE_BAR_SPACE_CSS = (["svh", "dvh", "vh"] as const)
  .map((unit) => {
    const name = unit === "vh" ? "screen" : unit;
    const height = `calc(100${unit} - ${BAR_HEIGHT_PX}px)`;
    return `.h-${name}{height:${height}}.min-h-${name}{min-height:${height}}`;
  })
  .join("");

const SYNC_DOT_CLASS: Record<WSStatus, string> = {
  connected: "bg-success",
  connecting: "bg-warning",
  reconnecting: "bg-destructive",
  offline: "bg-muted-foreground/50",
};

function useCurrentWorkspace() {
  const slug = useSyncExternalStore(subscribeToCurrentSlug, getCurrentSlug, () => null);
  const wsId = useSyncExternalStore(subscribeToCurrentSlug, getCurrentWsId, () => null);
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: Boolean(wsId),
  });
  const role = members?.find((m) => m.user_id === userId)?.role ?? null;
  return { slug, wsId, role };
}

/** Runtimes and agent tasks of the current workspace, from the app's own queries. */
function useWorkspaceAgents(wsId: string | null) {
  const { data: runtimes } = useQuery({ ...runtimeListOptions(wsId ?? ""), enabled: Boolean(wsId) });
  const { data: tasks } = useQuery({ ...agentTaskSnapshotOptions(wsId ?? ""), enabled: Boolean(wsId) });
  return { runtimes, tasks };
}

/** A bar reading that opens its detail panel. */
function Metric({
  label,
  value,
  tone = "normal",
  extra,
  wide,
  children,
}: {
  label: string;
  value: string;
  tone?: MetricTone;
  extra?: ReactNode;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <Popover>
      <PopoverTrigger
        data-testid={`dev-toolbar-${label.toLowerCase()}`}
        className="flex h-5 items-center gap-1 whitespace-nowrap rounded-md px-1 hover:bg-accent hover:text-foreground data-[popup-open]:bg-accent data-[popup-open]:text-foreground"
      >
        <span>{label}</span>
        <span className={cn("tabular-nums", TONE_CLASS[tone])}>{value}</span>
        {extra}
      </PopoverTrigger>
      <PopoverContent side="top" align="end" sideOffset={6} className={cn("gap-0 overflow-hidden p-0 text-caption", wide ? "w-[600px]" : "w-[440px]")}>
        {children}
      </PopoverContent>
    </Popover>
  );
}

/**
 * Development-only status bar pinned to the bottom of the window: which
 * client and backend this is, who you are in the current workspace, and live
 * client health. Each reading opens the recent history behind it. Platforms
 * mount it only in dev builds, behind an opt-in flag.
 */
export function DevToolbar({ build, apiUrl, route, onNavigate }: DevToolbarProps) {
  const [minimized, setMinimized] = useState(false);
  const sync = useWSStatus();
  const recording = useDevRecorder(route, sync);
  const now = useNow(1_000);
  const queryClient = useQueryClient();
  const { slug, wsId, role } = useCurrentWorkspace();
  const { runtimes, tasks } = useWorkspaceAgents(wsId);
  const blocked = useMainThreadBlocked(recording, now);
  const nav = useNavDuration(route);
  const heap = recording.memory.at(-1)?.bytes ?? null;
  const inFlight = recording.requests.filter((r) => r.durationMs === null).length;
  const failed = recording.requests.filter(requestFailed).length;
  const errors = recording.errors.length;
  const queries = queryClient.getQueryCache().getAll();
  const fetching = queries.filter((q) => q.state.fetchStatus === "fetching").length;
  const online = runtimes?.filter((r) => r.status === "online").length ?? 0;
  const running = activeTasks(tasks ?? []).filter((t) => t.status === "running").length;
  const openIssue = slug ? (issueId: string) => onNavigate(paths.workspace(slug).issueDetail(issueId)) : null;

  const syncDot = (
    <span className={cn("size-1.5 shrink-0 rounded-full", SYNC_DOT_CLASS[sync])} />
  );

  if (minimized) {
    return (
      <button
        type="button"
        data-testid="dev-toolbar-restore"
        onClick={() => setMinimized(false)}
        aria-label="Show dev toolbar"
        className="fixed bottom-0 left-1/2 z-[100] flex h-4 -translate-x-1/2 items-center gap-1 rounded-t-md border border-b-0 border-border bg-background px-2 font-mono text-micro text-muted-foreground hover:text-foreground"
      >
        {syncDot}
        <span>{build}</span>
        {errors > 0 && <span className="text-destructive">{errors}</span>}
        <ChevronUp className="size-3" />
      </button>
    );
  }

  return (
    <div
      data-testid="dev-toolbar"
      style={{ height: BAR_HEIGHT_PX }}
      className="fixed inset-x-0 bottom-0 z-[100] flex items-center gap-3 border-t border-border bg-background pl-2 pr-0.5 font-mono text-micro text-muted-foreground select-none"
    >
      <style>{RESERVE_BAR_SPACE_CSS}</style>
      <div className="flex min-w-0 items-center gap-3">
        <span className="flex items-center gap-1.5 text-foreground">
          {syncDot}
          {build}
        </span>
        <span className="whitespace-nowrap">{apiHost(apiUrl)}</span>
        {slug && <span className="truncate">{slug}</span>}
        {role && <span>{role}</span>}
        <span className="truncate">{route}</span>
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1">
        <Metric
          label="Mem"
          value={heap === null ? "—" : formatBytes(heap)}
          tone={heap === null ? "normal" : memoryTone(heap)}
        >
          <MemoryPanel recording={recording} />
        </Metric>
        <Metric
          label="Nav"
          value={nav === null ? "—" : `${Math.round(nav)}ms`}
          tone={nav === null ? "normal" : navTone(nav)}
        >
          <NavigationPanel recording={recording} />
        </Metric>
        <Metric
          label="Jank"
          value={blocked === null ? "—" : `${Math.round(blocked * 100)}%`}
          tone={blocked === null ? "normal" : jankTone(blocked)}
        >
          <LongTaskPanel recording={recording} blocked={blocked} />
        </Metric>
        <Metric
          label="Net"
          value={String(inFlight)}
          wide
          extra={failed > 0 && <span className="text-destructive">{`· ${failed} failed`}</span>}
        >
          <NetworkPanel recording={recording} apiUrl={apiUrl} workspaceSlug={slug} />
        </Metric>
        <Metric
          label="Cache"
          value={String(queries.length)}
          wide
          extra={
            // Fetches start and settle many times a second; a slot that came and
            // went would resize the chip and shift the whole bar each time.
            <span
              data-testid="dev-toolbar-cache-fetching"
              className={cn(
                "inline-block min-w-[3ch] tabular-nums",
                fetching > 0 ? "text-brand" : "text-faint-foreground",
              )}
            >
              {`↻${fetching}`}
            </span>
          }
        >
          <CachePanel now={now} />
        </Metric>
        <Metric
          label="Sync"
          value={sync}
          tone={sync === "reconnecting" ? "bad" : sync === "connecting" ? "warn" : "normal"}
        >
          <SyncPanel recording={recording} status={sync} />
        </Metric>
        <Metric label="Events" value={`${eventsInLastMinute(recording.frameCounts, now)}/m`} wide>
          <EventsPanel recording={recording} now={now} onOpenIssue={openIssue} />
        </Metric>
        {wsId && (
          <>
            <Metric
              label="Runtimes"
              value={runtimes ? `${online}/${runtimes.length}` : "—"}
              tone={runtimes && runtimes.length > 0 && online === 0 ? "bad" : "normal"}
              wide
            >
              <RuntimesPanel runtimes={runtimes} now={now} />
            </Metric>
            <Metric label="Tasks" value={tasks ? String(running) : "—"}>
              <TasksPanel tasks={tasks} wsId={wsId} now={now} onOpenIssue={openIssue} />
            </Metric>
          </>
        )}
        <Metric label="Errors" value={String(errors)} tone={errors > 0 ? "bad" : "normal"}>
          <ErrorsPanel recording={recording} />
        </Metric>
        <button
          type="button"
          onClick={() => setMinimized(true)}
          aria-label="Hide dev toolbar"
          className="flex size-5 items-center justify-center rounded-md hover:bg-accent hover:text-foreground"
        >
          <Minus className="size-3" />
        </button>
      </div>
    </div>
  );
}

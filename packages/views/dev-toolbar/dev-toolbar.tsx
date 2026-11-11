"use client";

import { useState, useSyncExternalStore } from "react";
import { useIsFetching, useIsMutating, useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import {
  getCurrentSlug,
  getCurrentWsId,
  subscribeToCurrentSlug,
} from "@multica/core/platform";
import { useWSStatus, type WSStatus } from "@multica/core/realtime";
import { memberListOptions } from "@multica/core/workspace/queries";
import { ChevronUp, Minus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  apiHost,
  formatBytes,
  jankTone,
  memoryTone,
  navTone,
  type MetricTone,
} from "./metrics";
import { useHeapBytes, useMainThreadBlocked, useNavDuration } from "./use-dev-metrics";

export interface DevToolbarProps {
  /** Which client this is, e.g. `web` or `desktop · staging`. */
  build: string;
  /** Backend the client talks to. */
  apiUrl: string;
  /** Current route as the platform's router sees it. */
  route: string;
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

const TONE_CLASS: Record<MetricTone, string> = {
  normal: "text-foreground",
  warn: "text-warning",
  bad: "text-destructive",
};

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
  return { slug, role };
}

function Metric({
  label,
  value,
  tone = "normal",
}: {
  label: string;
  value: string;
  tone?: MetricTone;
}) {
  return (
    <span className="flex items-center gap-1 whitespace-nowrap">
      <span>{label}</span>
      <span className={cn("tabular-nums", TONE_CLASS[tone])}>{value}</span>
    </span>
  );
}

/**
 * Development-only status bar pinned to the bottom of the window: which
 * client and backend this is, who you are in the current workspace, and live
 * client health. Platforms mount it only in dev builds, behind an opt-in flag.
 */
export function DevToolbar({ build, apiUrl, route }: DevToolbarProps) {
  const [minimized, setMinimized] = useState(false);
  const sync = useWSStatus();
  const { slug, role } = useCurrentWorkspace();
  const heap = useHeapBytes();
  const blocked = useMainThreadBlocked();
  const nav = useNavDuration(route);
  const inFlight = useIsFetching() + useIsMutating();

  const syncDot = (
    <span className={cn("size-1.5 shrink-0 rounded-full", SYNC_DOT_CLASS[sync])} />
  );

  if (minimized) {
    return (
      <button
        type="button"
        data-testid="dev-toolbar-restore"
        onClick={() => setMinimized(false)}
        // eslint-disable-next-line no-restricted-syntax -- developer-only control
        aria-label="Show dev toolbar"
        className="fixed bottom-0 left-1/2 z-[100] flex h-4 -translate-x-1/2 items-center gap-1 rounded-t-md border border-b-0 border-border bg-background px-2 font-mono text-micro text-muted-foreground hover:text-foreground"
      >
        {syncDot}
        <span>{build}</span>
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
      <div className="ml-auto flex shrink-0 items-center gap-3">
        <Metric
          label="Mem"
          value={heap === null ? "—" : formatBytes(heap)}
          tone={heap === null ? "normal" : memoryTone(heap)}
        />
        <Metric
          label="Nav"
          value={nav === null ? "—" : `${Math.round(nav)}ms`}
          tone={nav === null ? "normal" : navTone(nav)}
        />
        <Metric
          label="Jank"
          value={blocked === null ? "—" : `${Math.round(blocked * 100)}%`}
          tone={blocked === null ? "normal" : jankTone(blocked)}
        />
        <Metric label="Net" value={String(inFlight)} />
        <Metric
          label="Sync"
          value={sync}
          tone={sync === "reconnecting" ? "bad" : sync === "connecting" ? "warn" : "normal"}
        />
        <button
          type="button"
          onClick={() => setMinimized(true)}
          // eslint-disable-next-line no-restricted-syntax -- developer-only control
          aria-label="Hide dev toolbar"
          className="flex size-5 items-center justify-center rounded-md hover:bg-accent hover:text-foreground"
        >
          <Minus className="size-3" />
        </button>
      </div>
    </div>
  );
}

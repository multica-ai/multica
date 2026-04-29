"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Search, Server } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useUpdatableRuntimeIds } from "@multica/core/runtimes/hooks";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
import { useWSEvent } from "@multica/core/realtime";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { PageHeader } from "../../layout/page-header";
import { RuntimeList } from "./runtime-list";

type RuntimeFilter = "mine" | "all";
type HealthFilter = "all" | "online" | "recently_lost" | "offline" | "about_to_gc";

const HEALTH_ORDER: HealthFilter[] = [
  "all",
  "online",
  "recently_lost",
  "offline",
  "about_to_gc",
];

// Single source of truth for the 4-state chip visuals + tooltip copy.
// Thresholds come from `deriveRuntimeHealth`: 45s heartbeat window (server
// sweeper), 5 min "recently lost" cutoff, 6 day "about_to_gc" trigger,
// 7 day hard GC. Wording leans on what the user should *do*, not the
// internals of the sweeper — those live in the redesign doc.
const HEALTH_CHIP: Record<
  Exclude<HealthFilter, "all">,
  { label: string; dot: string; description: string }
> = {
  online: {
    label: "Online",
    dot: "bg-success",
    description: "Heartbeat received in the last 45s. Ready to dispatch tasks.",
  },
  recently_lost: {
    label: "Recently lost",
    dot: "bg-warning",
    description: "Lost contact under 5 minutes ago — often a brief network blip.",
  },
  offline: {
    label: "Offline",
    dot: "bg-muted-foreground/40",
    description: "No heartbeat for 5+ minutes. Restart the daemon or investigate the host.",
  },
  about_to_gc: {
    label: "About to GC",
    dot: "bg-destructive",
    description: "Offline 6+ days. Auto-deleted at 7 days unless it reconnects.",
  },
};

interface RuntimesPageProps {
  /** Desktop-only slot rendered above the runtimes table (e.g. local daemon card) */
  topSlot?: React.ReactNode;
  /**
   * Desktop-only signal: the bundled daemon is still booting / hasn't
   * registered with the server yet. Forwarded so the empty state can show
   * a "starting" indicator instead of the static "register a runtime" hint
   * during the boot window. Web omits this.
   */
  bootstrapping?: boolean;
}

// Re-render every 30s so derived health (recently_lost → offline transitions)
// catches up even when no underlying query data has changed.
function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function RuntimesPage({ topSlot, bootstrapping }: RuntimesPageProps = {}) {
  const isLoading = useAuthStore((s) => s.isLoading);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [scope, setScope] = useState<RuntimeFilter>("mine");
  const [healthFilter, setHealthFilter] = useState<HealthFilter>("all");
  const [search, setSearch] = useState("");

  // One unified cache per workspace: scope (Mine/All) is a view filter, not
  // a fetch dimension. Splitting on owner used to give us two TanStack cache
  // slots holding independent snapshots of the same runtime — switching scope
  // surfaced stale `last_seen_at` from whichever slot was older.
  const { data: runtimes = [], isLoading: fetching } = useQuery(
    runtimeListOptions(wsId),
  );
  const currentUserId = useAuthStore((s) => s.user?.id);

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const updatableIds = useUpdatableRuntimeIds(wsId);
  const now = useNowTick();

  // Apply scope first, then everything downstream (health counts, list filter)
  // operates on the post-scope set — so chip counts and filter results stay
  // consistent with what the user sees.
  const scopedRuntimes = useMemo(() => {
    if (scope !== "mine") return runtimes;
    if (!currentUserId) return [];
    return runtimes.filter((r) => r.owner_id === currentUserId);
  }, [runtimes, scope, currentUserId]);

  const healthCounts = useMemo(() => {
    const counts: Record<Exclude<HealthFilter, "all">, number> = {
      online: 0,
      recently_lost: 0,
      offline: 0,
      about_to_gc: 0,
    };
    for (const r of scopedRuntimes) {
      counts[deriveRuntimeHealth(r, now)] += 1;
    }
    return counts;
  }, [scopedRuntimes, now]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return scopedRuntimes.filter((r) => {
      if (healthFilter !== "all") {
        if (deriveRuntimeHealth(r, now) !== healthFilter) return false;
      }
      if (q) {
        const haystack = `${r.name} ${r.provider} ${r.device_info ?? ""}`.toLowerCase();
        if (!haystack.includes(q)) return false;
      }
      return true;
    });
  }, [scopedRuntimes, healthFilter, search, now]);

  if (isLoading || fetching) return <RuntimesPageSkeleton />;

  const totalCount = runtimes.length;
  const scopedTotal = scopedRuntimes.length;
  const showEmpty = totalCount === 0 && !bootstrapping;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar totalCount={totalCount} />

      <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
        {topSlot}

        {showEmpty ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState />
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border bg-background">
            <CardToolbar
              search={search}
              setSearch={setSearch}
              scope={scope}
              setScope={setScope}
            />
            <FilterChipsRow
              healthFilter={healthFilter}
              setHealthFilter={setHealthFilter}
              healthCounts={healthCounts}
              total={scopedTotal}
            />
            {filtered.length === 0 ? (
              <NoMatchesState search={search} healthFilter={healthFilter} scope={scope} bootstrapping={bootstrapping} />
            ) : (
              <RuntimeList
                runtimes={filtered}
                updatableIds={updatableIds}
                now={now}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Header bar — minimal: only icon + title + count, matching Skills.
// Page-level actions (Search, scope, filter) live in the card below.
// ---------------------------------------------------------------------------

function PageHeaderBar({ totalCount }: { totalCount: number }) {
  return (
    <PageHeader className="px-5">
      <div className="flex items-center gap-2">
        <Server className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">Runtimes</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        {/* Tagline sits right next to the title — same flex group, single
            sentence + docs link. Hidden below md so it never collides with
            the title on narrow screens. */}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          Machines and cloud workers running CLI sessions for your agents.{" "}
          <a
            href="https://multica.ai/docs/runtimes"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            Learn more →
          </a>
        </p>
      </div>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Intro block — sits between the page header and the table card. Mirrors
// Skills' two-paragraph pattern: a one-liner plus a brand-accented callout
// pinning down a single non-obvious fact about the surface.
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Card toolbar — search + scope toggle + live indicator. Skills puts its
// search and filter buttons here; we follow the same convention so the card
// owns its own interactions.
// ---------------------------------------------------------------------------

function CardToolbar({
  search,
  setSearch,
  scope,
  setScope,
}: {
  search: string;
  setSearch: (v: string) => void;
  scope: RuntimeFilter;
  setScope: (v: RuntimeFilter) => void;
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search runtimes…"
          className="h-8 w-64 pl-8 text-sm"
        />
      </div>
      <ScopeSegment value={scope} onChange={setScope} />
      <Tooltip>
        <TooltipTrigger
          render={
            <div className="ml-auto inline-flex cursor-default select-none items-center gap-1.5 text-xs text-muted-foreground">
              <span className="relative inline-flex h-2 w-2 items-center justify-center">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success/60" />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-success" />
              </span>
              Live
            </div>
          }
        />
        <TooltipContent side="top">
          Real-time updates · offline detection up to 75s
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

function ScopeSegment({
  value,
  onChange,
}: {
  value: RuntimeFilter;
  onChange: (v: RuntimeFilter) => void;
}) {
  // Mine first — that's the default selection and the more frequent
  // scope (your own runtimes), so it lives in the leading slot. Mirrors
  // the Agents page convention.
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      <button
        onClick={() => onChange("mine")}
        className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
          value === "mine"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground"
        }`}
      >
        Mine
      </button>
      <button
        onClick={() => onChange("all")}
        className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
          value === "all"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground"
        }`}
      >
        All
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Filter chips — 4 health states + "All", each with a tooltip explaining
// what the state actually means in operational terms. Counts come from the
// pre-filter set so users can see "what would happen" before clicking.
// ---------------------------------------------------------------------------

function FilterChipsRow({
  healthFilter,
  setHealthFilter,
  healthCounts,
  total,
}: {
  healthFilter: HealthFilter;
  setHealthFilter: (v: HealthFilter) => void;
  healthCounts: Record<Exclude<HealthFilter, "all">, number>;
  total: number;
}) {
  return (
    <div className="flex shrink-0 items-center gap-2 border-b px-4 py-2.5">
      {HEALTH_ORDER.map((key) => {
        const count = key === "all" ? total : healthCounts[key];
        const visual = key === "all" ? null : HEALTH_CHIP[key];
        const description =
          key === "all" ? "All runtimes in this view" : visual!.description;
        return (
          <HealthChip
            key={key}
            active={healthFilter === key}
            onClick={() => setHealthFilter(key)}
            label={visual?.label ?? "All"}
            count={count}
            dotClass={visual?.dot}
            description={description}
          />
        );
      })}
    </div>
  );
}

// Mirrors Agents' `PresenceChip` — same `Button outline + size sm` shell so
// any future polish to the chip token cascades to both surfaces. The active
// state uses `bg-accent text-accent-foreground hover:bg-accent/80`, matching
// Skills' filter chip selection.
function HealthChip({
  active,
  onClick,
  label,
  count,
  dotClass,
  description,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  dotClass?: string;
  description: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            onClick={onClick}
            className={
              active
                ? "bg-accent text-accent-foreground hover:bg-accent/80"
                : "text-muted-foreground"
            }
          >
            {dotClass && (
              <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
            )}
            <span>{label}</span>
            <span className="font-mono tabular-nums text-muted-foreground/70">
              {count}
            </span>
          </Button>
        }
      />
      <TooltipContent side="top">{description}</TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Empty state — shown when zero runtimes have ever registered in this
// workspace. Different from "filter matches nothing" (NoMatchesState).
// ---------------------------------------------------------------------------

function EmptyState() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Server className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">No runtimes yet</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        Runtimes register automatically when a daemon connects. Run{" "}
        <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
          multica daemon start
        </code>{" "}
        on your machine, or invite a teammate whose daemon is already running.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// No matches state — runtimes exist but the current filter combination
// hides all of them. Keeps the user oriented by reflecting *which* filters
// are in play.
// ---------------------------------------------------------------------------

function NoMatchesState({
  search,
  healthFilter,
  scope,
  bootstrapping,
}: {
  search: string;
  healthFilter: HealthFilter;
  scope: RuntimeFilter;
  bootstrapping?: boolean;
}) {
  if (bootstrapping) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center">
        <Server className="h-8 w-8 animate-pulse text-muted-foreground/40" />
        <p className="text-sm text-muted-foreground">Starting local runtime…</p>
        <p className="max-w-xs text-xs text-muted-foreground/70">
          This usually takes a few seconds. Your daemon is registering with the workspace.
        </p>
      </div>
    );
  }

  const hasSearch = search.length > 0;
  const hasHealthFilter = healthFilter !== "all";
  const hasScope = scope === "mine";

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
      <Search className="h-8 w-8 text-muted-foreground/40" />
      <p className="text-sm">No matches</p>
      <p className="max-w-xs text-xs">
        {hasSearch
          ? `No runtimes match "${search}"${hasHealthFilter || hasScope ? " in this filter" : ""}.`
          : "No runtimes match this filter."}{" "}
        Try widening the scope or clearing filters.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading skeleton — laid out the same as the real page (header + intro
// + card) so the layout doesn't jump on first paint.
// ---------------------------------------------------------------------------

function RuntimesPageSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <Skeleton className="h-4 w-24" />
      </PageHeader>
      <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
        <div className="space-y-3 pl-4">
          <Skeleton className="h-5 w-full max-w-2xl rounded-md" />
          <Skeleton className="h-14 w-full max-w-3xl rounded-md" />
        </div>
        <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border">
          <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
            <Skeleton className="h-8 w-64 rounded-md" />
            <Skeleton className="h-7 w-20 rounded-md" />
          </div>
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-md" />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export default RuntimesPage;
export type { RuntimesPageProps };

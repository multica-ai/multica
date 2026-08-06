"use client";

import { useCallback, useEffect, useState  } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import {
  runtimeDisplayLabel,
} from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { MikaIntro } from "../components/mika-intro";
import {
  StepFooter,
} from "../components/step-shell";
import { useRuntimePicker } from "../components/use-runtime-picker";
import { MikaRuntimeChoice } from "../../runtimes/components/mika-runtime-choice";
import { useT } from "../../i18n";

/**
 * Step 3 (desktop) — connect a runtime.
 *
 * Owns the full window: DragStrip + 3-region app shell (header /
 * scrolling middle / sticky footer), a single column. Built to mirror
 * Step 1 questionnaire's shell so the onboarding flow reads as one
 * continuous surface.
 *
 * Data layer (`useRuntimePicker`): TanStack Query polls every 2s
 * while empty; `daemon:register` WS event invalidates instantly;
 * default selection prefers online, falls back to first.
 *
 * Web routes to `StepPlatformFork` instead — it owns its own
 * runtime picker embedded under the CLI expand.
 */
export function StepRuntimeConnect({
  wsId,
  wsSlug,
  onNext,
  onRefresh,
  runtimesPending,
  currentUserId,
}: {
  wsId: string;
  /** Slug of the target workspace. Sent explicitly so the runtime list reads
   *  the workspace being set up rather than whichever one the app is currently
   *  showing. */
  wsSlug?: string;
  onNext: (runtime: AgentRuntime | null, model?: string) => void | Promise<void>;
  /** Runtime picker labels rows by owner; injected for the same reason. */
  currentUserId?: string | null;
  /** Platform-level rescan hook. Desktop wires this to restart the
   *  bundled daemon so a freshly-installed CLI shows up — otherwise the
   *  daemon's PATH probe runs once at boot and never re-probes. */
  onRefresh?: () => void | Promise<void>;
  /** Desktop-only signal: the local daemon is still booting or is known to
   *  have agent CLIs on this host that haven't finished registering yet.
   *  While true, the step keeps showing the scanning skeleton past the normal
   *  timeout instead of flashing the "no runtime found" empty state — that
   *  empty state is a false negative when the daemon is mid-probe (MUL-5119).
   *  Web omits it and keeps the plain wall-clock timeout. */
  runtimesPending?: boolean;
}) {
  const { runtimes, selected, selectedId, setSelectedId } =
    useRuntimePicker(wsId, wsSlug);

  return (
    <FancyView
      wsId={wsId}
      runtimes={runtimes}
      selected={selected}
      selectedId={selectedId}
      setSelectedId={setSelectedId}
      onNext={onNext}
      currentUserId={currentUserId}
      onRefresh={onRefresh}
      runtimesPending={runtimesPending}
    />
  );
}

// ============================================================
// Fancy desktop view
// ============================================================

type Phase = "scanning" | "found" | "empty";

/** Idle ms before an empty list flips from "scanning" to "empty" — unless the
 *  platform reports runtimes are still pending (see `runtimesPending`). */
const EMPTY_TIMEOUT_MS = 5000;

/** Absolute ceiling: even while the platform still reports runtimes pending,
 *  fall back to the empty exits after this so a wedged version probe can never
 *  hang the step on the scanning skeleton forever. */
const EMPTY_HARD_TIMEOUT_MS = 20000;

function FancyView({
  wsId,
  runtimes,
  selected,
  selectedId,
  setSelectedId,
  onNext,
  onRefresh,
  runtimesPending,
  currentUserId,
}: {
  wsId: string;
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  onNext: (runtime: AgentRuntime | null, model?: string) => void | Promise<void>;
  onRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
  /** Runtime picker labels rows by owner; injected so this step does not read
   *  the auth store (six tests render it without one). */
  currentUserId?: string | null;
}) {
  const { t } = useT("onboarding");
  const qc = useQueryClient();

  // Decide when an empty runtime list stops being "still scanning" and becomes
  // the genuine "no runtime" exits. Two timers run while the list is empty:
  //
  //   - soft (EMPTY_TIMEOUT_MS): the normal budget. Once it fires we flip to
  //     empty UNLESS `runtimesPending` says the platform (desktop daemon) is
  //     still booting or mid-probe — registration on a host with several CLIs
  //     can outlast the soft budget, and flashing "no runtime found" while the
  //     daemon is still working is a false negative (MUL-5119).
  //   - hard (EMPTY_HARD_TIMEOUT_MS): an absolute ceiling so a wedged probe
  //     that never resolves `runtimesPending` back to false can't pin the step
  //     on the scanning skeleton forever.
  //
  // `scanEpoch` resets both timers when the user hits Refresh, so a
  // freshly-installed CLI gets another scanning window before falling back to
  // the empty state.
  const [scanEpoch, setScanEpoch] = useState(0);
  const [softTimedOut, setSoftTimedOut] = useState(false);
  const [hardTimedOut, setHardTimedOut] = useState(false);
  useEffect(() => {
    if (runtimes.length > 0) return;
    setSoftTimedOut(false);
    setHardTimedOut(false);
    const soft = window.setTimeout(() => setSoftTimedOut(true), EMPTY_TIMEOUT_MS);
    const hard = window.setTimeout(
      () => setHardTimedOut(true),
      EMPTY_HARD_TIMEOUT_MS,
    );
    return () => {
      window.clearTimeout(soft);
      window.clearTimeout(hard);
    };
  }, [runtimes.length, scanEpoch]);

  const phase: Phase =
    runtimes.length > 0
      ? "found"
      : hardTimedOut || (softTimedOut && runtimesPending !== true)
        ? "empty"
        : "scanning";

  const onlineCount = runtimes.filter((r) => r.status === "online").length;

  const [submitting, setSubmitting] = useState(false);
  const [model, setModel] = useState("");
  const [refreshing, setRefreshing] = useState(false);

  // Refresh triggers a re-scan: restart the daemon (if the platform
  // wired `onRefresh`) so its PATH probe runs again, invalidate the
  // runtime query, and reset the empty-state timeout so the user sees
  // the scanning skeleton instead of the empty exits while the daemon
  // boots back up.
  const handleRefresh = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      if (onRefresh) await onRefresh();
      await qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      setScanEpoch((n) => n + 1);
    } finally {
      setRefreshing(false);
    }
  }, [onRefresh, qc, wsId, refreshing]);

  // Skip is always available — regardless of phase. Hitting Skip routes
  // through the runtime-less branch, which creates one focused self-serve
  // onboarding issue instead of seeding the old starter project.
  const handleSkip = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onNext(null);
    } finally {
      setSubmitting(false);
    }
  };
  // Starting with Mika only makes sense when a runtime is selected.
  const canContinue = phase === "found" && selected !== null;
  const handleContinue = async () => {
    if (!canContinue || submitting) return;
    setSubmitting(true);
    try {
      await onNext(selected, model);
    } finally {
      setSubmitting(false);
    }
  };

  const footerHint =
    phase === "found" && selected
      ? t(($) => $.step_runtime.hint_selected, {
          name: runtimeDisplayLabel(selected),
        })
      : phase === "found"
        ? t(($) => $.step_runtime.hint_pick)
        : phase === "scanning"
          ? t(($) => $.step_runtime.hint_waiting)
          : t(($) => $.step_runtime.hint_skip_or_refresh);

  return (
    <>
      {/* key=phase forces a remount on phase transition so the
          `animate-onboarding-enter` animation replays — otherwise CSS
          only runs on initial mount and scanning→found would be a
          hard cut. */}
      <div
        key={phase}
        className="animate-onboarding-enter flex flex-col gap-8 pt-2 sm:pt-6"
      >
        <MikaIntro />

        {phase === "scanning" && <ScanningView />}
        {phase === "found" && (
          <FoundView
            runtimes={runtimes}
            selectedId={selectedId}
            onSelect={setSelectedId}
            onlineCount={onlineCount}
            onRefresh={handleRefresh}
            refreshing={refreshing}
            model={model}
            onModelChange={setModel}
            currentUserId={currentUserId ?? null}
          />
        )}
        {phase === "empty" && (
          <EmptyView
            onSkip={handleSkip}
            onRefresh={handleRefresh}
            refreshing={refreshing}
          />
        )}

        {/* Footer action bar. The controls are phase-scoped so no dead or
            duplicated affordance ever shows:
              - Skip: shown while scanning / found. The empty phase owns its
                own prominent Skip card, so the footer Skip is dropped there
                to avoid two "Skip for now" buttons on one screen.
              - Continue: only actionable once a runtime is picked, so
                it renders only in the found phase instead of sitting
                permanently disabled through scanning / empty. */}
      </div>

      <StepFooter hint={footerHint}>
        {phase === "found" && (
          <Button
            className="w-full"
            disabled={!canContinue || submitting}
            onClick={handleContinue}
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            {t(($) => $.step_runtime.continue)}
          </Button>
        )}
        {phase !== "empty" && (
          <Button
            variant="ghost"
            className="w-full"
            disabled={submitting}
            onClick={handleSkip}
          >
            {t(($) => $.step_runtime.skip)}
          </Button>
        )}
      </StepFooter>
    </>
  );
}

// ------------------------------------------------------------
// Phase views (inline — all three share the same 620px column)
// ------------------------------------------------------------

function ScanningView() {
  const { t } = useT("onboarding");
  return (
    <div>
      <h2 className="text-title-sm font-medium tracking-tight text-foreground">
        {t(($) => $.step_runtime.scanning_headline)}
      </h2>
      <p className="mt-2 text-body text-muted-foreground">
        {t(($) => $.step_runtime.scanning_lede_prefix)}
        <span className="font-medium text-foreground">{"Claude Code"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Codex"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Cursor"}</span>
        {t(($) => $.step_runtime.scanning_lede_suffix)}
      </p>
      <div className="mt-10 grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
        <SkeletonRuntimeCard />
        <SkeletonRuntimeCard />
      </div>
    </div>
  );
}

function FoundView({
  runtimes,
  selectedId,
  onSelect,
  onlineCount,
  onRefresh,
  refreshing,
  model,
  onModelChange,
  currentUserId,
}: {
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onlineCount: number;
  onRefresh: () => void;
  refreshing: boolean;
  model: string;
  onModelChange: (value: string) => void;
  currentUserId: string | null;
}) {
  const { t } = useT("onboarding");
  const total = runtimes.length;
  const statusLabel =
    onlineCount === total
      ? t(($) => $.step_runtime.status_all_online)
      : onlineCount === 0
        ? t(($) => $.step_runtime.status_none_online)
        : t(($) => $.step_runtime.status_n_online, { count: onlineCount });
  const statusTone =
    onlineCount === 0 ? "text-muted-foreground" : "text-success";

  return (
    <div>
      <h2 className="text-title-sm font-medium tracking-tight text-foreground">
        {t(($) => $.step_runtime.found_headline)}
      </h2>
      <p className="mt-2 text-body text-muted-foreground">
        {t(($) => $.step_runtime.found_lede)}
      </p>

      <div className="mt-8 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-muted/60 px-4 py-2.5 text-caption">
        <span className="font-semibold text-foreground">
          {t(($) => $.step_runtime.runtime_count, { count: total })}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className={cn("flex items-center gap-1", statusTone)}>
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full",
              onlineCount === 0 ? "bg-muted-foreground/40" : "bg-success",
            )}
            aria-hidden
          />
          {statusLabel}
        </span>
        <RefreshButton
          onClick={onRefresh}
          refreshing={refreshing}
          className="ml-auto"
        />
      </div>

      <div className="mt-6 flex flex-col gap-4">
        <MikaRuntimeChoice
          runtimes={runtimes}
          currentUserId={currentUserId}
          value={{ runtimeId: selectedId ?? "", model }}
          onChange={(next) => {
            if (next.runtimeId !== selectedId) onSelect(next.runtimeId);
            if (next.model !== model) onModelChange(next.model);
          }}
        />
      </div>
    </div>
  );
}

function EmptyView({
  onSkip,
  onRefresh,
  refreshing,
}: {
  onSkip: () => void;
  onRefresh: () => void;
  refreshing: boolean;
}) {
  const { t } = useT("onboarding");

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <h2 className="text-title-sm font-medium tracking-tight text-foreground">
          {t(($) => $.step_runtime.empty_headline)}
        </h2>
        <RefreshButton
          onClick={onRefresh}
          refreshing={refreshing}
          className="mt-2 shrink-0"
        />
      </div>
      <p className="mt-2 text-body text-muted-foreground">
        {t(($) => $.step_runtime.empty_lede_prefix)}
        <span className="font-medium text-foreground">{"Claude Code"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Codex"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Cursor"}</span>
        {t(($) => $.step_runtime.empty_lede_suffix)}
      </p>

      <div className="mt-10 flex flex-col gap-3.5">
        <EmptyCard
          title={t(($) => $.step_runtime.empty_skip_title)}
          subtitle={t(($) => $.step_runtime.empty_skip_subtitle)}
          actionLabel={t(($) => $.step_runtime.empty_skip_action)}
          onAction={onSkip}
        />

        <ComingSoonCard
          title={t(($) => $.step_runtime.empty_waitlist_title)}
          subtitle={t(($) => $.step_runtime.empty_waitlist_subtitle)}
          badgeLabel={t(($) => $.step_runtime.empty_waitlist_action)}
        />
      </div>
    </div>
  );
}

/**
 * Static, non-interactive variant of EmptyCard used for the cloud-computer
 * row. The card is dimmed and the pill is rendered as a badge so the user
 * understands the option exists but isn't actionable yet. Mirrors the
 * "Coming soon" treatment on the web platform fork.
 */
function ComingSoonCard({
  title,
  subtitle,
  badgeLabel,
}: {
  title: string;
  subtitle: string;
  badgeLabel: string;
}) {
  return (
    <div
      aria-disabled
      className="flex items-center justify-between gap-4 rounded-lg border border-dashed bg-muted/20 px-5 py-4 opacity-70"
    >
      <div className="min-w-0">
        <div className="text-body font-medium text-foreground">{title}</div>
        <p className="mt-1 text-caption leading-[1.55] text-muted-foreground">
          {subtitle}
        </p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center rounded-full border bg-background px-3 py-1.5 text-caption font-medium uppercase tracking-wide text-muted-foreground"
      >
        {badgeLabel}
      </span>
    </div>
  );
}

function RefreshButton({
  onClick,
  refreshing,
  className,
}: {
  onClick: () => void;
  refreshing: boolean;
  className?: string;
}) {
  const { t } = useT("onboarding");
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={refreshing}
      onClick={onClick}
      className={className}
    >
      <RefreshCw
        className={cn("h-3.5 w-3.5", refreshing && "animate-spin")}
        aria-hidden
      />
      {refreshing
        ? t(($) => $.step_runtime.refreshing)
        : t(($) => $.step_runtime.refresh)}
    </Button>
  );
}

/**
 * Card with a prominent right-side button. Mirrors the ForkAlt pattern
 * from the web fork step — whole card is clickable, but the pill is
 * the visual affordance that signals "this is a button".
 */
function EmptyCard({
  title,
  subtitle,
  actionLabel,
  onAction,
}: {
  title: string;
  subtitle: string;
  actionLabel: string;
  onAction: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onAction}
      className="group flex items-center justify-between gap-4 rounded-lg border bg-card px-5 py-4 text-left transition-colors hover:border-foreground/30 hover:bg-muted/30"
    >
      <div className="min-w-0">
        <div className="text-body font-medium text-foreground">{title}</div>
        <p className="mt-1 text-caption leading-[1.55] text-muted-foreground">
          {subtitle}
        </p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full border bg-background px-4 py-2 text-label font-medium text-foreground transition-colors group-hover:border-foreground group-hover:bg-foreground group-hover:text-background"
      >
        {actionLabel}
        <ArrowRight className="h-3.5 w-3.5" />
      </span>
    </button>
  );
}

// ------------------------------------------------------------
// Card components
// ------------------------------------------------------------

function SkeletonRuntimeCard() {
  return (
    <div
      aria-hidden
      className="flex animate-pulse items-center gap-3 rounded-lg border bg-card p-4"
    >
      <div className="h-7 w-7 shrink-0 rounded-md bg-muted" />
      <div className="flex-1 space-y-2">
        <div className="h-3 w-28 rounded bg-muted" />
        <div className="h-2.5 w-16 rounded bg-muted/70" />
      </div>
      <div className="h-4 w-4 shrink-0 rounded-full border-[1.5px] border-muted" />
    </div>
  );
}

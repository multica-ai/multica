"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Loader2, RefreshCw } from "lucide-react";
import { captureEvent, setPersonProperties } from "@multica/core/analytics";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RuntimeAsidePanel } from "../components/runtime-aside-panel";
import { useRuntimePicker } from "../components/use-runtime-picker";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";

/**
 * Step 3 (desktop) — connect a runtime.
 *
 * Owns the full window: DragStrip + 3-region app shell (header /
 * scrolling middle / sticky footer) on the left, permanent
 * educational aside on the right. Built to mirror Step 1
 * questionnaire's shell so the onboarding flow reads as one
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
  onNext,
  onBack,
  onRefresh,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  /** Platform-level rescan hook. Desktop wires this to restart the
   *  bundled daemon so a freshly-installed CLI shows up — otherwise the
   *  daemon's PATH probe runs once at boot and never re-probes. */
  onRefresh?: () => void | Promise<void>;
}) {
  const { runtimes, selected, selectedId, setSelectedId } =
    useRuntimePicker(wsId);

  return (
    <FancyView
      wsId={wsId}
      runtimes={runtimes}
      selected={selected}
      selectedId={selectedId}
      setSelectedId={setSelectedId}
      onNext={onNext}
      onBack={onBack}
      onRefresh={onRefresh}
    />
  );
}

// ============================================================
// Fancy desktop view
// ============================================================

type Phase = "scanning" | "found" | "empty";

/** Input ms before an empty list flips from "scanning" to "empty". */
const EMPTY_TIMEOUT_MS = 5000;

function FancyView({
  wsId,
  runtimes,
  selected,
  selectedId,
  setSelectedId,
  onNext,
  onBack,
  onRefresh,
}: {
  wsId: string;
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  onRefresh?: () => void | Promise<void>;
}) {
  const { t } = useT("onboarding");
  const qc = useQueryClient();
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  // Flip to "empty" only after we've waited long enough for the daemon
  // to report. The 5s budget covers the bundled daemon's typical 1–3s
  // boot; anything past that is a genuine "no runtime" situation and we
  // switch from scanning skeletons to the skip / refresh exits.
  // `scanEpoch` resets the timer when the user hits Refresh, so a
  // freshly-installed CLI gets another scanning window before falling
  // back to the empty state.
  const [scanEpoch, setScanEpoch] = useState(0);
  const [hasTimedOut, setHasTimedOut] = useState(false);
  useEffect(() => {
    if (runtimes.length > 0) return;
    setHasTimedOut(false);
    const id = window.setTimeout(() => setHasTimedOut(true), EMPTY_TIMEOUT_MS);
    return () => window.clearTimeout(id);
  }, [runtimes.length, scanEpoch]);

  const phase: Phase =
    runtimes.length > 0 ? "found" : hasTimedOut ? "empty" : "scanning";

  const onlineCount = runtimes.filter((r) => r.status === "online").length;

  // One-shot analytics event when the scan window resolves. Answers the
  // question "did the user actually have any AI CLI installed on this
  // machine when they hit Step 3" — currently unanswerable from the
  // existing funnel because a zero-CLI daemon fails to register at all,
  // so `runtime_registered` is silent on that cohort. Emitting from here
  // (rather than the daemon) keeps the signal in sync with what the UI
  // actually showed the user: "scanning → found" vs "scanning → empty"
  // after the 5s grace period.
  const detectStartRef = useRef<number | null>(null);
  if (detectStartRef.current === null) {
    detectStartRef.current =
      typeof performance !== "undefined" ? performance.now() : Date.now();
  }
  const detectedEmittedRef = useRef(false);
  useEffect(() => {
    if (detectedEmittedRef.current) return;
    if (phase === "scanning") return;
    detectedEmittedRef.current = true;

    const providers = Array.from(
      new Set(runtimes.map((r) => r.provider).filter(Boolean)),
    ).sort();
    const now =
      typeof performance !== "undefined" ? performance.now() : Date.now();
    const detectMs = Math.round(now - (detectStartRef.current ?? now));

    captureEvent("onboarding_runtime_detected", {
      source: "onboarding",
      surface: "step3_desktop",
      workspace_id: wsId,
      outcome: phase,
      runtime_count: runtimes.length,
      online_count: onlineCount,
      providers,
      has_claude: providers.includes("claude"),
      has_codex: providers.includes("codex"),
      has_cursor: providers.includes("cursor"),
      detect_ms: detectMs,
    });

    setPersonProperties({
      has_any_cli: runtimes.length > 0,
      detected_cli_count: runtimes.length,
    });
  }, [phase, runtimes, onlineCount]);

  const [submitting, setSubmitting] = useState(false);
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
      detectedEmittedRef.current = false;
      detectStartRef.current =
        typeof performance !== "undefined" ? performance.now() : Date.now();
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
  // Continue only makes sense when a runtime is selected. Otherwise
  // there's nothing to pass to Step 4.
  const canContinue = phase === "found" && selected !== null;
  const handleContinue = async () => {
    if (!canContinue || submitting) return;
    setSubmitting(true);
    try {
      await onNext(selected);
    } finally {
      setSubmitting(false);
    }
  };

  const footerHint =
    phase === "found" && selected
      ? t(($) => $.step_runtime.hint_selected, { name: selected.name })
      : phase === "found"
        ? t(($) => $.step_runtime.hint_pick)
        : phase === "scanning"
          ? t(($) => $.step_runtime.hint_waiting)
          : t(($) => $.step_runtime.hint_skip_or_refresh);

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />

        {/* Header — Back + horizontal step indicator */}
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t(($) => $.common.back)}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="runtime" />
          </div>
        </header>

        {/* Scrollable middle — content changes by phase but always wraps
            at max-w-[620px] so the 2-column runtime grid has room to
            breathe without stretching into readability territory.

            Skip + Continue sit inline directly below the phase view
            (not in a sticky bottom footer) so the action bar stays
            close to the form content and the page doesn't leave a
            large dead zone when the runtime list is short. */}
        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          {/* key=phase forces a remount on phase transition so the
              `animate-onboarding-enter` animation replays — otherwise CSS
              only runs on initial mount and scanning→found would be a
              hard cut. */}
          <div
            key={phase}
            className="animate-onboarding-enter mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14"
          >
            {phase === "scanning" && <ScanningView />}
            {phase === "found" && (
              <FoundView
                runtimes={runtimes}
                selectedId={selectedId}
                onSelect={setSelectedId}
                onlineCount={onlineCount}
                onRefresh={handleRefresh}
                refreshing={refreshing}
              />
            )}
            {phase === "empty" && (
              <EmptyView
                onSkip={() => onNext(null)}
                onRefresh={handleRefresh}
                refreshing={refreshing}
              />
            )}

            <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
              <span
                aria-live="polite"
                className="mr-auto text-xs text-muted-foreground"
              >
                {footerHint}
              </span>
              <div className="flex items-center gap-2">
                <Button
                  size="lg"
                  variant="secondary"
                  disabled={submitting}
                  onClick={handleSkip}
                >
                  {t(($) => $.step_runtime.skip)}
                </Button>
                <Button
                  size="lg"
                  disabled={!canContinue || submitting}
                  onClick={handleContinue}
                >
                  {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                  {t(($) => $.common.continue)}
                  <ArrowRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        </main>
      </div>

      {/* Right — always-visible educational aside. "You picked" subsection
          only appears when there's a selection; the other two stay constant. */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <RuntimeAsidePanel />
        </div>
      </aside>
    </div>
  );
}

// ------------------------------------------------------------
// Phase views (inline — all three share the same 620px column)
// ------------------------------------------------------------

function ScanningView() {
  const { t } = useT("onboarding");
  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        {t(($) => $.step_runtime.scanning_headline)}
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.scanning_lede_prefix)}
        <span className="font-medium text-foreground">{"Claude Code"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Codex"}</span>
        {", "}
        <span className="font-medium text-foreground">{"Cursor"}</span>
        {t(($) => $.step_runtime.scanning_lede_suffix)}
      </p>
      <div className="mt-10 grid grid-cols-1 gap-2.5 sm:grid-cols-2">
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
}: {
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onlineCount: number;
  onRefresh: () => void;
  refreshing: boolean;
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
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        {t(($) => $.step_runtime.found_headline)}
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_runtime.found_lede)}
      </p>

      <div className="mt-8 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-muted/60 px-4 py-2.5 text-xs">
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

      <div className="mt-6 grid grid-cols-1 gap-2.5 sm:grid-cols-2">
        {runtimes.map((rt) => (
          <RuntimeCard
            key={rt.id}
            runtime={rt}
            selected={rt.id === selectedId}
            onSelect={() => onSelect(rt.id)}
          />
        ))}
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
        <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
          {t(($) => $.step_runtime.empty_headline)}
        </h1>
        <RefreshButton
          onClick={onRefresh}
          refreshing={refreshing}
          className="mt-2 shrink-0"
        />
      </div>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
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
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
          {subtitle}
        </p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center rounded-full border bg-background px-3 py-1.5 text-[12px] font-medium uppercase tracking-wide text-muted-foreground"
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
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <p className="mt-1 text-[12.5px] leading-[1.55] text-muted-foreground">
          {subtitle}
        </p>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full border bg-background px-4 py-2 text-[13px] font-medium text-foreground transition-colors group-hover:border-foreground group-hover:bg-foreground group-hover:text-background"
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

function RuntimeCard({
  runtime,
  selected,
  onSelect,
}: {
  runtime: AgentRuntime;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("onboarding");
  const online = runtime.status === "online";

  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex items-center gap-3 rounded-lg border bg-card p-4 text-left transition-colors",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/20",
      )}
    >
      <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent/30">
        <ProviderLogo provider={runtime.provider} className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-foreground">
          {runtime.name}
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground">
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full",
              online ? "bg-success" : "bg-muted-foreground/40",
            )}
            aria-hidden
          />
          {online ? t(($) => $.step_runtime.online_label) : t(($) => $.step_runtime.offline_label)}
        </div>
      </div>
      <RadioMark selected={selected} />
    </button>
  );
}

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

function RadioMark({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "relative inline-block h-4 w-4 shrink-0 rounded-full border-[1.5px] transition-colors",
        selected ? "border-foreground" : "border-border",
      )}
    >
      {selected && (
        <span className="absolute inset-[3px] rounded-full bg-foreground" />
      )}
    </span>
  );
}

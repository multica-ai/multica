"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import type { AgentRuntime } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RuntimeAsidePanel } from "../components/runtime-aside-panel";
import { useRuntimePicker } from "../components/use-runtime-picker";
import { CloudWaitlistExpand } from "../components/cloud-waitlist-expand";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

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
  onWaitlistSubmitted,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  /** Parent-level latch used to label the onboarding completion path
   *  as `cloud_waitlist` when the user ends up skipping this step
   *  after submitting the waitlist form. */
  onWaitlistSubmitted?: () => void;
}) {
  const { runtimes, selected, selectedId, setSelectedId } =
    useRuntimePicker(wsId);

  return (
    <FancyView
      runtimes={runtimes}
      selected={selected}
      selectedId={selectedId}
      setSelectedId={setSelectedId}
      onNext={onNext}
      onBack={onBack}
      onWaitlistSubmitted={onWaitlistSubmitted}
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
  runtimes,
  selected,
  selectedId,
  setSelectedId,
  onNext,
  onBack,
  onWaitlistSubmitted,
}: {
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  onWaitlistSubmitted?: () => void;
}) {
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  // Flip to "empty" only after we've waited long enough for the daemon
  // to report. The 5s budget covers the bundled daemon's typical 1–3s
  // boot; anything past that is a genuine "no runtime" situation and we
  // switch from scanning skeletons to the skip / cloud-waitlist exits.
  const [hasTimedOut, setHasTimedOut] = useState(false);
  useEffect(() => {
    if (runtimes.length > 0) return;
    const t = window.setTimeout(() => setHasTimedOut(true), EMPTY_TIMEOUT_MS);
    return () => window.clearTimeout(t);
  }, [runtimes.length]);

  const phase: Phase =
    runtimes.length > 0 ? "found" : hasTimedOut ? "empty" : "scanning";

  const onlineCount = runtimes.filter((r) => r.status === "online").length;

  const [submitting, setSubmitting] = useState(false);
  // Cloud waitlist submission state lives here (rather than in EmptyView)
  // so it survives phase flips — e.g. a runtime coming online after the
  // user has already submitted the waitlist form.
  const [waitlistSubmitted, setWaitlistSubmitted] = useState(false);

  // Skip is always available — regardless of phase. Hitting Skip routes
  // the flow through the self-serve branch (agent=null), which still
  // completes onboarding and seeds a Getting Started project.
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
      ? `Selected: ${selected.name}`
      : phase === "found"
        ? "Pick a runtime above to continue."
        : phase === "scanning"
          ? "Waiting for the first result…"
          : waitlistSubmitted
            ? "You're on the waitlist — skip to keep exploring."
            : "Skip to enter your workspace, or join the cloud waitlist above.";

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
              Back
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
            breathe without stretching into readability territory. */}
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
              />
            )}
            {phase === "empty" && (
              <EmptyView
                waitlistSubmitted={waitlistSubmitted}
                onWaitlistSubmitted={() => {
                  setWaitlistSubmitted(true);
                  onWaitlistSubmitted?.();
                }}
                onSkip={() => onNext(null)}
              />
            )}
          </div>
        </main>

        {/* Sticky footer — Skip (always) on the left, hint + Continue
            (gated on runtime selection) on the right. Skip is the
            self-serve exit: onNext(null) → bootstrap runs the no-agent
            branch, onboarding still completes. */}
        <footer className="flex shrink-0 items-center justify-end gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span
            aria-live="polite"
            className="mr-auto text-xs text-muted-foreground"
          >
            {footerHint}
          </span>
          <Button
            variant="secondary"
            disabled={submitting}
            onClick={handleSkip}
          >
            Skip for now
          </Button>
          <Button
            size="lg"
            disabled={!canContinue || submitting}
            onClick={handleContinue}
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            Continue
            <ArrowRight className="h-4 w-4" />
          </Button>
        </footer>
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
  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        Looking for your tools…
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        Multica drives local AI coding tools like{" "}
        <span className="font-medium text-foreground">Claude Code</span>,{" "}
        <span className="font-medium text-foreground">Codex</span>,{" "}
        <span className="font-medium text-foreground">Cursor</span>, and
        others. We&apos;re waiting to hear back from your machine about
        which ones are installed.
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
}: {
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onlineCount: number;
}) {
  const total = runtimes.length;
  const statusLabel =
    onlineCount === total
      ? "all online"
      : onlineCount === 0
        ? "none online"
        : `${onlineCount} online`;
  const statusTone =
    onlineCount === 0 ? "text-muted-foreground" : "text-success";

  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        We found your runtimes.
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        We scanned your machine for AI coding tools you&apos;ve already
        set up. Pick one for your first agent.
      </p>

      {/* Summary strip — trust signal ("we really did scan") */}
      <div className="mt-8 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-muted/60 px-4 py-2.5 text-xs">
        <span className="font-semibold text-foreground">
          {total} runtime{total === 1 ? "" : "s"}
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
  waitlistSubmitted,
  onWaitlistSubmitted,
  onSkip,
}: {
  waitlistSubmitted: boolean;
  onWaitlistSubmitted: () => void;
  onSkip: () => void;
}) {
  // Two exits: "Skip for now" (enter the workspace in read-only mode)
  // or "Join waitlist" (capture interest in the hosted runtime we
  // haven't shipped yet). We deliberately don't link out to Claude
  // Code / Codex / Cursor here — those are other companies' products,
  // and nudging the user to install one would frame Multica as a
  // launcher for them rather than a product that runs them.
  const [waitlistOpen, setWaitlistOpen] = useState(false);

  return (
    <div>
      <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
        No supported tools detected.
      </h1>
      <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
        Multica drives local AI coding tools like{" "}
        <span className="font-medium text-foreground">Claude Code</span>,{" "}
        <span className="font-medium text-foreground">Codex</span>,{" "}
        <span className="font-medium text-foreground">Cursor</span>, and
        others — we didn&apos;t find any on this machine. Install one and
        come back, or pick a path below.
      </p>

      <div className="mt-10 flex flex-col gap-3.5">
        <EmptyCard
          title="Skip for now"
          subtitle="Enter your workspace in read-only mode. Agents can't execute tasks until a runtime connects — but you can still browse, plan, and invite teammates."
          actionLabel="Skip"
          onAction={onSkip}
        />

        <EmptyCard
          title="Join the cloud runtime waitlist"
          subtitle="We'll host the runtime for you — no local install, no setup. Not live yet; click to leave your email and get notified."
          actionLabel={waitlistSubmitted ? "On the waitlist" : "Join waitlist"}
          onAction={() => setWaitlistOpen(true)}
        />
      </div>

      <Dialog
        open={waitlistOpen}
        onOpenChange={(o) => (o ? null : setWaitlistOpen(false))}
      >
        <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>Join the cloud runtime waitlist</DialogTitle>
            <DialogDescription>
              Cloud runtimes aren&apos;t live yet. Leave your email and
              we&apos;ll email you when they are.
            </DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 overflow-y-auto pt-2">
            <CloudWaitlistExpand
              submitted={waitlistSubmitted}
              onSubmitted={onWaitlistSubmitted}
            />
          </div>

          <DialogFooter>
            <Button variant="ghost" onClick={() => setWaitlistOpen(false)}>
              {waitlistSubmitted ? "Close" : "Cancel"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
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
          {online ? "online" : "offline"}
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


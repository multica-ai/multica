"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, Download } from "lucide-react";
import {
  captureDownloadIntent,
  captureEvent,
  setPersonProperties,
} from "@multica/core/analytics";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RuntimeAsidePanel } from "../components/runtime-aside-panel";
import { CompactRuntimeRow } from "../components/compact-runtime-row";
import { useRuntimePicker } from "../components/use-runtime-picker";
import { CloudWaitlistExpand } from "../components/cloud-waitlist-expand";
import { useT } from "../../i18n";

/**
 * Step 3 on **web**. The user is in a browser and hasn't downloaded
 * the desktop app yet, so we can't scan their machine for runtimes.
 * This screen is a fan-out: three clearly clickable cards, each with
 * an explicit right-side button that says what clicking does:
 *
 *   1. **Download desktop** — primary card, black bg, "Download" pill.
 *      Opens the installer in a new tab; the user finishes onboarding
 *      inside the desktop app.
 *   2. **Install the CLI** — alt card, "Show steps" pill → opens a
 *      dialog containing the real install instructions + live runtime
 *      probe. When a runtime appears and the user selects it, the
 *      dialog's "Connect & continue" button fires `onNext(runtime)`
 *      and advances the flow.
 *   3. **Cloud waitlist** — alt card, "Join waitlist" pill → opens a
 *      dialog with an email + reason form. Submitting is pure interest
 *      capture; the dialog doesn't advance the flow. The user then
 *      closes the dialog and can hit Skip in the footer.
 *
 * Footer is simplified — no Continue button, since the CLI dialog
 * owns that advancement itself. Only Skip remains.
 */

type DialogState = "cli" | "cloud" | null;

// Single canonical download destination — the /download page owns
// OS + arch detection, the All-Platforms matrix, release-note links,
// and the CLI / Cloud alternates. Kept in sync with landing-hero.tsx
// and landing footer nav, both of which target the same path.
const DOWNLOAD_PAGE_URL = "/download";

export function StepPlatformFork({
  wsId,
  onNext,
  onBack,
  cliInstructions,
  onWaitlistSubmitted,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  /** Platform-specific CLI install card, rendered inside the CLI dialog. */
  cliInstructions?: ReactNode;
  /** Parent-level latch used to label the onboarding completion path
   *  as `cloud_waitlist` when the user ends up skipping Step 3 after
   *  submitting the waitlist form. */
  onWaitlistSubmitted?: () => void;
}) {
  const { t } = useT("onboarding");
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const [dialog, setDialog] = useState<DialogState>(null);
  const [downloaded, setDownloaded] = useState(false);
  const [waitlistSubmitted, setWaitlistSubmitted] = useState(false);

  // Platform signal retained purely for PostHog dimensions — the UI
  // no longer branches on it (Windows / Linux desktop installers now
  // ship, so all three platforms get the same card). Computed
  // lazily; SSR-safe because handlers only run client-side.
  const isMac =
    typeof navigator !== "undefined" &&
    (/Mac|iPhone|iPad|iPod/i.test(navigator.platform || "") ||
      /Mac OS X/i.test(navigator.userAgent || ""));

  const picker = useRuntimePicker(wsId);

  const pickDesktop = () => {
    window.open(DOWNLOAD_PAGE_URL, "_blank", "noopener,noreferrer");
    setDownloaded(true);
    // Step-3-scoped path selection event (kept for existing funnels);
    // `source: "step3"` future-proofs if the event is reused from
    // another surface later.
    captureEvent("onboarding_runtime_path_selected", {
      workspace_id: wsId,
      path: "download_desktop",
      source: "onboarding",
      surface: "step3",
      is_mac: isMac,
    });
    // Cross-surface Desktop intent event — also fires from landing
    // hero / footer / login / Welcome. Enables the top-of-funnel
    // split without retrofitting `onboarding_runtime_path_selected`
    // to non-onboarding contexts.
    captureDownloadIntent("step3");
  };

  const handleOpenCli = () => {
    setDialog("cli");
    captureEvent("onboarding_runtime_path_selected", {
      workspace_id: wsId,
      path: "cli",
      source: "onboarding",
      surface: "step3",
      is_mac: isMac,
    });
    setPersonProperties({ platform_preference: "web" });
  };

  const handleOpenCloud = () => {
    setDialog("cloud");
    captureEvent("onboarding_runtime_path_selected", {
      workspace_id: wsId,
      path: "cloud_waitlist",
      source: "onboarding",
      surface: "step3",
      is_mac: isMac,
    });
  };

  const handleCliConnect = () => {
    if (!picker.selected) return;
    setDialog(null);
    onNext(picker.selected);
  };

  const footerHint = (() => {
    if (waitlistSubmitted) {
      return t(($) => $.step_platform.hint_waitlist);
    }
    if (downloaded) {
      return t(($) => $.step_platform.hint_downloaded);
    }
    return t(($) => $.step_platform.hint_default);
  })();

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />

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

        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {t(($) => $.step_platform.eyebrow)}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {t(($) => $.step_platform.headline)}
            </h1>
            <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
              {t(($) => $.step_platform.lede)}
            </p>

            <div className="mt-10 flex max-w-[560px] flex-col gap-3.5">
              <ForkPrimary onClick={pickDesktop} downloaded={downloaded} />

              <ForkAlt
                title={t(($) => $.step_platform.cli_title)}
                subtitle={t(($) => $.step_platform.cli_subtitle)}
                actionLabel={t(($) => $.step_platform.cli_action)}
                onAction={handleOpenCli}
              />

              <ForkAlt
                title={t(($) => $.step_platform.cloud_title)}
                subtitle={t(($) => $.step_platform.cloud_subtitle)}
                actionLabel={
                  waitlistSubmitted
                    ? t(($) => $.step_platform.cloud_action_done)
                    : t(($) => $.step_platform.cloud_action)
                }
                onAction={handleOpenCloud}
              />
            </div>
          </div>
        </main>

        {/* Footer — hint on the left, Skip on the right. Advancement
            for the CLI path is owned by the CLI dialog's own
            "Connect & continue" button; Skip is the self-serve exit. */}
        <footer className="flex shrink-0 items-center justify-between gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span
            aria-live="polite"
            className="text-xs text-muted-foreground"
          >
            {footerHint}
          </span>
          <Button variant="secondary" onClick={() => onNext(null)}>
            {t(($) => $.step_runtime.skip)}
          </Button>
        </footer>
      </div>

      {/* Right — always-visible aside */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <RuntimeAsidePanel />
        </div>
      </aside>

      <CliInstallDialog
        open={dialog === "cli"}
        onClose={() => setDialog(null)}
        onConnect={handleCliConnect}
        runtimes={picker.runtimes}
        selectedId={picker.selectedId}
        onSelect={picker.setSelectedId}
        hasRuntimes={picker.hasRuntimes}
        canConnect={picker.selected !== null}
        selectedName={picker.selected?.name ?? null}
        cliInstructions={cliInstructions}
      />

      <CloudWaitlistDialog
        open={dialog === "cloud"}
        onClose={() => setDialog(null)}
        submitted={waitlistSubmitted}
        onSubmitted={() => {
          setWaitlistSubmitted(true);
          onWaitlistSubmitted?.();
        }}
      />
    </div>
  );
}

// ------------------------------------------------------------
// Fork cards
// ------------------------------------------------------------

function ForkPrimary({
  onClick,
  downloaded,
}: {
  onClick: () => void;
  downloaded: boolean;
}) {
  const { t } = useT("onboarding");
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group flex items-center justify-between gap-4 rounded-xl bg-foreground px-6 py-5 text-left text-background transition-transform",
        "hover:-translate-y-0.5",
      )}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2 text-[17px] font-medium tracking-tight">
          <Download className="h-4 w-4" aria-hidden />
          {downloaded
            ? t(($) => $.step_platform.download_title_after)
            : t(($) => $.step_platform.download_title)}
        </div>
        <div className="mt-1 text-[13px] text-background/60">
          {downloaded
            ? t(($) => $.step_platform.download_subtitle_after)
            : t(($) => $.step_platform.download_subtitle)}
        </div>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-background/10 px-4 py-2 text-[13px] font-medium transition-colors group-hover:bg-background/20"
      >
        {t(($) => $.step_platform.download_button)}
        <ArrowRight className="h-3.5 w-3.5" />
      </span>
    </button>
  );
}

/**
 * Alt card with an explicit right-side action pill. The whole card is
 * clickable (so you can hit the title/subtitle too), but the pill is the
 * visual anchor — it's what tells the user "this card is a button".
 * Pressing it opens a dialog that owns the real content + action.
 */
function ForkAlt({
  title,
  subtitle,
  actionLabel,
  onAction,
}: {
  title: string;
  subtitle: ReactNode;
  actionLabel: ReactNode;
  onAction: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border bg-card px-5 py-4">
      <div className="min-w-0">
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <div className="mt-1 text-[12.5px] leading-[1.5] text-muted-foreground">
          {subtitle}
        </div>
      </div>
      <Button
        variant="outline"
        size="sm"
        className="shrink-0"
        onClick={onAction}
      >
        {actionLabel}
      </Button>
    </div>
  );
}

// ------------------------------------------------------------
// CLI install dialog
// ------------------------------------------------------------

/**
 * Modal dialog for the CLI install path. Contains the real install
 * instructions card (via the `cliInstructions` slot) plus the live
 * runtime probe. Owns its own "Connect & continue" advancement — when
 * a runtime has registered and the user picks it, clicking that button
 * closes the dialog and fires the parent's `onConnect`.
 */
function CliInstallDialog({
  open,
  onClose,
  onConnect,
  runtimes,
  selectedId,
  onSelect,
  hasRuntimes,
  canConnect,
  selectedName,
  cliInstructions,
}: {
  open: boolean;
  onClose: () => void;
  onConnect: () => void;
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  hasRuntimes: boolean;
  canConnect: boolean;
  selectedName: string | null;
  cliInstructions?: ReactNode;
}) {
  const { t } = useT("onboarding");
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.step_platform.cli_dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.step_platform.cli_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pt-2">
          {cliInstructions}

          {hasRuntimes ? (
            <>
              <div className="flex items-center gap-2 pt-1 text-sm">
                <div className="h-2 w-2 rounded-full bg-success" />
                <span className="font-medium">
                  {t(($) => $.step_platform.runtimes_connected, { count: runtimes.length })}
                </span>
              </div>
              {/* Cap the runtime list at ~4 rows visible, scroll the rest.
                  Keeps the commands above always reachable even when
                  a user has many machines registered. */}
              <div className="flex max-h-[240px] flex-col gap-2 overflow-y-auto">
                {runtimes.map((rt) => (
                  <CompactRuntimeRow
                    key={rt.id}
                    runtime={rt}
                    selected={rt.id === selectedId}
                    onSelect={() => onSelect(rt.id)}
                  />
                ))}
              </div>
            </>
          ) : (
            <CliWaitingStatus dialogOpen={open} />
          )}
        </div>

        <DialogFooter className="flex items-center justify-between gap-3 sm:justify-between">
          {/* Hint is only useful AFTER a runtime has registered — "pick
              one" / "selected X". While still waiting, the body's
              CliWaitingStatus already conveys the live-listening state,
              so an additional "Waiting..." footer line is duplication. */}
          <span className="text-xs text-muted-foreground">
            {hasRuntimes
              ? canConnect && selectedName
                ? t(($) => $.step_runtime.hint_selected, { name: selectedName })
                : t(($) => $.step_platform.cli_dialog_pick_hint)
              : null}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" onClick={onClose}>
              {t(($) => $.step_runtime.dialog_cancel)}
            </Button>
            <Button disabled={!canConnect} onClick={onConnect}>
              {t(($) => $.step_platform.cli_dialog_connect)}
              <ArrowRight className="h-4 w-4" />
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Format a seconds count as `m:ss` (e.g. 75 → "1:15"). Inline helper —
 * no existing utility matches this format (agent-live-card's
 * formatElapsed uses "1m 15s" style, not suitable for a ticking clock).
 */
function formatElapsed(seconds: number) {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/**
 * Waiting state for the CLI dialog — shown until the first daemon
 * registers. We can't actually observe the install / login / daemon-
 * start phases from the frontend (they happen in the user's terminal
 * and browser), so the best we can do is:
 *
 *   1. Confirm "we're listening" — a pulsing green dot + m:ss timer
 *      signals an active WS subscription (useRuntimePicker is already
 *      subscribed to `daemon:register`). This is what tells the user
 *      "the system isn't frozen, it's waiting for your daemon".
 *   2. Progressively reveal troubleshooting hints as elapsed time
 *      crosses thresholds — so a user who stalls mid-setup gets
 *      useful guidance without being dogpiled at t=0.
 *   3. At the 90s+ "stalled" tier, point the user at alternate paths
 *      (Skip / Cloud waitlist) — parallels desktop's EmptyView, which
 *      already exposes the same two exits when no runtime registers.
 *
 * Elapsed-time counter only ticks while the dialog is open so reopen
 * after closing resets the staging.
 */
function CliWaitingStatus({ dialogOpen }: { dialogOpen: boolean }) {
  const { t } = useT("onboarding");
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!dialogOpen) {
      setElapsed(0);
      return;
    }
    const id = window.setInterval(() => {
      setElapsed((e) => e + 1);
    }, 1000);
    return () => window.clearInterval(id);
  }, [dialogOpen]);

  // Stage thresholds are rough — `multica setup` typical flow is
  //   ~1s save config → browser-tab auth (user-driven, 5–30s) →
  //   ~2s daemon boot → immediate WS register. So under 15s means
  //   "still normal", 15–45s means "probably stuck on browser auth",
  //   45–90s means "probably an error in the terminal", 90s+ means
  //   "nothing's coming through, suggest alt paths" (the stalled tier
  //   parallels desktop StepRuntimeConnect's EmptyView — by that point
  //   it's worth pointing the user at Skip or Cloud waitlist).
  const stage: "normal" | "midway" | "slow" | "stalled" =
    elapsed < 15
      ? "normal"
      : elapsed < 45
        ? "midway"
        : elapsed < 90
          ? "slow"
          : "stalled";

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-4">
      <div className="flex items-center gap-2 text-sm">
        {/* Pulsing green dot signals active WS subscription — the
            useRuntimePicker hook is already subscribed to `daemon:register`,
            this is the visual confirmation that "we're listening". */}
        <span
          aria-hidden
          className="inline-block size-2 shrink-0 rounded-full bg-success animate-pulse"
        />
        <span className="font-medium text-foreground">
          {t(($) => $.step_platform.live_listening)}
        </span>
        <span className="ml-auto font-mono text-xs tabular-nums text-muted-foreground">
          {formatElapsed(elapsed)}
        </span>
      </div>

      <p
        aria-live="polite"
        className="text-[12.5px] leading-[1.55] text-muted-foreground"
      >
        {stage === "normal" && (
          <>
            {t(($) => $.step_platform.stage_normal_prefix)}
            <span className="font-mono">{"multica setup"}</span>
            {t(($) => $.step_platform.stage_normal_suffix)}
          </>
        )}
        {stage === "midway" && (
          <>
            {t(($) => $.step_platform.stage_midway_prefix)}
            <span className="font-mono">{"multica setup"}</span>
            {t(($) => $.step_platform.stage_midway_suffix)}
          </>
        )}
        {stage === "slow" && (
          <>
            {t(($) => $.step_platform.stage_slow_prefix)}
            <span className="font-mono">{"multica setup"}</span>
            {t(($) => $.step_platform.stage_slow_suffix)}
          </>
        )}
        {stage === "stalled" && (
          <>
            {t(($) => $.step_platform.stage_stalled_prefix)}
            <span className="font-medium text-foreground">{t(($) => $.step_platform.stage_stalled_term)}</span>
            {t(($) => $.step_platform.stage_stalled_suffix)}
          </>
        )}
      </p>
    </div>
  );
}

// ------------------------------------------------------------
// Cloud waitlist dialog
// ------------------------------------------------------------

/**
 * Modal dialog for the cloud waitlist path. Wraps the shared
 * `CloudWaitlistExpand` form. Submitting it records interest — the
 * dialog does NOT advance the onboarding flow. After submit, the user
 * closes the dialog and can hit Skip in the footer.
 */
function CloudWaitlistDialog({
  open,
  onClose,
  submitted,
  onSubmitted,
}: {
  open: boolean;
  onClose: () => void;
  submitted: boolean;
  onSubmitted: () => void;
}) {
  const { t } = useT("onboarding");
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.step_runtime.dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.step_runtime.dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto pt-2">
          <CloudWaitlistExpand
            submitted={submitted}
            onSubmitted={onSubmitted}
          />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {submitted
              ? t(($) => $.step_runtime.dialog_close)
              : t(($) => $.step_runtime.dialog_cancel)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

"use client";

import { useEffect, useState, type ReactNode } from "react";
import { ArrowRight, Download, Loader2 } from "lucide-react";
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
import type { AgentRuntime } from "@multica/core/types";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import {
  StepFooter,
  StepHeading,
} from "../components/step-shell";
import {
  MikaRuntimeChoice,
  type MikaRuntimeSelection,
} from "../../runtimes/components/mika-runtime-choice";
import { useRuntimePicker } from "../components/use-runtime-picker";
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
 *   3. **Cloud computer** — alt card, "Coming soon" badge. Not yet
 *      available; rendered as a static, non-actionable preview.
 *
 * Footer is simplified — no Continue button, since the CLI dialog
 * owns that advancement itself. Only Skip remains.
 */

type DialogState = "cli" | null;

// Single canonical download destination — the /download page owns
// OS + arch detection, the All-Platforms matrix, release-note links,
// and the CLI / Cloud alternates. Kept in sync with landing-hero.tsx
// and landing footer nav, both of which target the same path.
const DOWNLOAD_PAGE_URL = "/download";

export function StepPlatformFork({
  wsId,
  wsSlug,
  onNext,
  cliInstructions,
}: {
  wsId: string;
  /** Slug of the target workspace. Sent explicitly so the runtime list reads
   *  the workspace being set up rather than whichever one the app is currently
   *  showing. */
  wsSlug?: string;
  onNext: (runtime: AgentRuntime | null, model?: string) => void | Promise<void>;
  /** Platform-specific CLI install card, rendered inside the CLI dialog. */
  cliInstructions?: ReactNode;
}) {
  const { t } = useT("onboarding");

  const [dialog, setDialog] = useState<DialogState>(null);
  const [connecting, setConnecting] = useState(false);
  const [model, setModel] = useState("");

  const picker = useRuntimePicker(wsId, wsSlug);

  const pickDesktop = () => {
    // No post-click state. `noopener` makes window.open return null by spec
    // whether it opened or was blocked, so this cannot know which happened —
    // and the copy it used to flip to ("Opened in a new tab.") was a claim we
    // had no way to stand behind. The card states the intent up front
    // instead, which is true either way.
    window.open(DOWNLOAD_PAGE_URL, "_blank", "noopener,noreferrer");
  };

  const handleOpenCli = () => {
    setDialog("cli");
  };

  const handleCliConnect = async () => {
    if (!picker.selected || connecting) return;
    setConnecting(true);
    try {
      await onNext(picker.selected, model || undefined);
      setDialog(null);
    } finally {
      setConnecting(false);
    }
  };

  const footerHint = t(($) => $.step_platform.hint_default);

  return (
    <>
      <div className="flex flex-col gap-8 pt-2 sm:pt-6">
        {/* The eyebrow read "Connect a computer" directly above a headline
            that starts with the same three words. The block has no eyebrow
            slot and the rail already names the step, so it goes. */}
        <StepHeading
          title={t(($) => $.step_platform.headline)}
          description={t(($) => $.step_platform.lede)}
        />

        <div className="flex flex-col gap-2">
          <ForkPrimary onClick={pickDesktop} />

          <ForkAlt
            title={t(($) => $.step_platform.cli_title)}
            subtitle={t(($) => $.step_platform.cli_subtitle)}
            actionLabel={t(($) => $.step_platform.cli_action)}
            onAction={handleOpenCli}
          />

          <ForkAlt
            title={t(($) => $.step_platform.cloud_title)}
            subtitle={t(($) => $.step_platform.cloud_subtitle)}
            actionLabel={t(($) => $.step_platform.cloud_action)}
            disabled
          />
        </div>

      </div>

      {/* Advancement for the CLI path is owned by the CLI dialog's own
          "Connect & continue" button; Skip creates the single self-serve
          onboarding issue. */}
      <StepFooter hint={footerHint}>
        <Button
          variant="ghost"
          className="w-full"
          onClick={() => onNext(null)}
        >
          {t(($) => $.step_runtime.skip)}
        </Button>
      </StepFooter>

    <CliInstallDialog
      open={dialog === "cli"}
      onClose={() => setDialog(null)}
      onConnect={handleCliConnect}
      runtimes={picker.runtimes}
      choice={{ runtimeId: picker.selectedId ?? "", model }}
      onChoiceChange={(next) => {
        if (next.runtimeId !== picker.selectedId) {
          picker.setSelectedId(next.runtimeId);
        }
        setModel(next.model);
      }}
      hasRuntimes={picker.hasRuntimes}
      canConnect={picker.selected !== null}
      selectedName={
        picker.selected ? runtimeDisplayLabel(picker.selected) : null
      }
      connecting={connecting}
      cliInstructions={cliInstructions}
    />
    </>
  );
}

// ------------------------------------------------------------
// Fork cards
// ------------------------------------------------------------

function ForkPrimary({ onClick }: { onClick: () => void }) {
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
        <div className="flex items-center gap-2 text-title font-medium tracking-tight">
          <Download className="h-4 w-4" aria-hidden />
          {t(($) => $.step_platform.download_title)}
        </div>
        <div className="mt-1 text-label text-background/60">
          {t(($) => $.step_platform.download_subtitle)}
        </div>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-background/10 px-4 py-2 text-label font-medium transition-colors group-hover:bg-background/20"
      >
        {t(($) => $.step_platform.download_button)}
        <ArrowRight className="h-3.5 w-3.5" />
      </span>
    </button>
  );
}

/**
 * Alt card with a right-side action. When `disabled`, the action
 * renders as a static badge (used for "Coming soon" paths that aren't
 * yet wired up); otherwise it's an outline button that fires
 * `onAction` and typically opens a dialog.
 */
function ForkAlt({
  title,
  subtitle,
  actionLabel,
  onAction,
  disabled = false,
}: {
  title: string;
  subtitle: ReactNode;
  actionLabel: ReactNode;
  onAction?: () => void;
  disabled?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-4 rounded-lg border bg-card px-5 py-4",
        disabled && "opacity-70",
      )}
    >
      <div className="min-w-0">
        <div className="text-body font-medium text-foreground">{title}</div>
        <div className="mt-1 text-caption leading-[1.5] text-muted-foreground">
          {subtitle}
        </div>
      </div>
      {disabled ? (
        <span className="shrink-0 rounded-full border bg-muted px-3 py-1 text-caption font-medium text-muted-foreground">
          {actionLabel}
        </span>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          onClick={onAction}
        >
          {actionLabel}
        </Button>
      )}
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
  choice,
  onChoiceChange,
  hasRuntimes,
  canConnect,
  selectedName,
  connecting,
  cliInstructions,
}: {
  open: boolean;
  onClose: () => void;
  onConnect: () => void | Promise<void>;
  runtimes: AgentRuntime[];
  choice: MikaRuntimeSelection;
  onChoiceChange: (next: MikaRuntimeSelection) => void;
  hasRuntimes: boolean;
  canConnect: boolean;
  selectedName: string | null;
  connecting: boolean;
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
              <div className="flex items-center gap-2 pt-1 text-body">
                <div className="h-2 w-2 rounded-full bg-success" />
                <span className="font-medium">
                  {t(($) => $.step_platform.runtimes_connected, { count: runtimes.length })}
                </span>
              </div>
              {/* Cap the runtime list at ~4 rows visible, scroll the rest.
                  Keeps the commands above always reachable even when
                  a user has many machines registered. */}
              <MikaRuntimeChoice
                layout="list"
                runtimes={runtimes}
                value={choice}
                onChange={onChoiceChange}
                disabled={connecting}
              />
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
          <span className="text-caption text-muted-foreground">
            {hasRuntimes
              ? canConnect && selectedName
                ? t(($) => $.step_runtime.hint_selected, { name: selectedName })
                : t(($) => $.step_platform.cli_dialog_pick_hint)
              : null}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" onClick={onClose}>
              {t(($) => $.common.cancel)}
            </Button>
            <Button disabled={!canConnect || connecting} onClick={onConnect}>
              {connecting && <Loader2 className="h-4 w-4 animate-spin" />}
              {t(($) => $.step_runtime.continue)}
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
      <div className="flex items-center gap-2 text-body">
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
        <span className="ml-auto font-mono text-caption tabular-nums text-muted-foreground">
          {formatElapsed(elapsed)}
        </span>
      </div>

      <p
        aria-live="polite"
        className="text-caption leading-[1.55] text-muted-foreground"
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

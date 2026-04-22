"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, Download } from "lucide-react";
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

// Kept in sync with the Landing page's "Download Desktop" CTA
// (apps/web/features/landing/components/landing-hero.tsx). Both point
// at the GitHub Releases latest page, which redirects to the current
// tag and lists the platform-specific .dmg / .zip assets. There is no
// multica.ai-hosted download page — do not invent one here.
const DESKTOP_DOWNLOAD_URL =
  "https://github.com/multica-ai/multica/releases/latest";

export function StepPlatformFork({
  wsId,
  onNext,
  onBack,
  cliInstructions,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
  /** Platform-specific CLI install card, rendered inside the CLI dialog. */
  cliInstructions?: ReactNode;
}) {
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const [dialog, setDialog] = useState<DialogState>(null);
  const [downloaded, setDownloaded] = useState(false);
  const [waitlistSubmitted, setWaitlistSubmitted] = useState(false);

  // Desktop app currently ships macOS binaries only (see electron-builder
  // config + release workflow). On non-mac platforms we keep the card
  // visible but muted, and redirect users to the CLI path.
  // Default true to match SSR; useEffect corrects post-hydration.
  const [isMac, setIsMac] = useState(true);
  useEffect(() => {
    if (typeof navigator === "undefined") return;
    const p = navigator.platform || "";
    const ua = navigator.userAgent || "";
    setIsMac(/Mac|iPhone|iPad|iPod/i.test(p) || /Mac OS X/i.test(ua));
  }, []);

  const picker = useRuntimePicker(wsId);

  const pickDesktop = () => {
    window.open(DESKTOP_DOWNLOAD_URL, "_blank", "noopener,noreferrer");
    setDownloaded(true);
  };

  const handleCliConnect = () => {
    if (!picker.selected) return;
    setDialog(null);
    onNext(picker.selected);
  };

  const footerHint = (() => {
    if (waitlistSubmitted) {
      return "You're on the waitlist — pick Skip to keep exploring.";
    }
    if (downloaded) {
      return "Downloading… finish setup in the desktop app, or pick another path.";
    }
    if (!isMac) {
      return "Install the CLI to connect a runtime, or skip for now.";
    }
    return "Pick a path above — or skip and configure a runtime later.";
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
              Back
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
              Step 3 · Runtime
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              Connect a runtime.
            </h1>
            <p className="mt-4 max-w-[560px] text-[15.5px] leading-[1.55] text-muted-foreground">
              A runtime is what actually runs your agents&apos; work. Pick
              how you&apos;d like to set one up.
            </p>

            <div className="mt-10 flex max-w-[560px] flex-col gap-3.5">
              <ForkPrimary
                onClick={pickDesktop}
                downloaded={downloaded}
                isMac={isMac}
              />

              <ForkAlt
                title="Install the CLI"
                subtitle="Run the Multica daemon yourself — a couple of terminal commands."
                actionLabel="Show steps"
                onAction={() => setDialog("cli")}
              />

              <ForkAlt
                title="Cloud runtime"
                subtitle="We host it for you. Not live yet — leave your email and we'll let you know."
                actionLabel={
                  waitlistSubmitted ? "On the list" : "Join waitlist"
                }
                onAction={() => setDialog("cloud")}
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
            Skip for now
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
        onSubmitted={() => setWaitlistSubmitted(true)}
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
  isMac,
}: {
  onClick: () => void;
  downloaded: boolean;
  isMac: boolean;
}) {
  // On non-mac platforms we can't deliver a binary yet. The card stays
  // visible so users understand the desktop app exists, but it's muted
  // and steers them down to the CLI path.
  if (!isMac) {
    return (
      <div
        aria-disabled="true"
        className="flex cursor-not-allowed items-center justify-between gap-4 rounded-xl border bg-muted/40 px-6 py-5 text-left"
      >
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[17px] font-medium tracking-tight text-muted-foreground">
            <Download className="h-4 w-4" aria-hidden />
            Desktop app — macOS only for now
          </div>
          <div className="mt-1 text-[13px] text-muted-foreground/80">
            Windows and Linux builds are on the way. In the meantime,
            install the CLI below — it takes about two minutes.
          </div>
        </div>
        <span
          aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground"
        >
          <ArrowRight className="h-4 w-4" />
        </span>
      </div>
    );
  }

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
          {downloaded ? "Downloading Multica…" : "Download the desktop app"}
        </div>
        <div className="mt-1 text-[13px] text-background/60">
          {downloaded
            ? "Opened in a new tab. Finish setup inside the desktop app."
            : "macOS · runtime bundled — detects your tools automatically, nothing to install."}
        </div>
      </div>
      <span
        aria-hidden
        className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-background/10 px-4 py-2 text-[13px] font-medium transition-colors group-hover:bg-background/20"
      >
        Download
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
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>Install the CLI</DialogTitle>
          <DialogDescription>
            Runs the same daemon the desktop app bundles — you install
            it yourself. This screen watches for it to come online.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 pt-2">
          {cliInstructions}

          {/* Live probe. Shows a staged waiting message with elapsed-
              time fallbacks while no runtime is detected; flips to
              a success list once the daemon registers via WS. */}
          {hasRuntimes ? (
            <>
              <div className="flex items-center gap-2 pt-1 text-sm">
                <div className="h-2 w-2 rounded-full bg-success" />
                <span className="font-medium">
                  {runtimes.length} runtime{runtimes.length > 1 ? "s" : ""}{" "}
                  connected
                </span>
              </div>
              <div className="flex flex-col gap-2">
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
          <span className="text-xs text-muted-foreground">
            {canConnect && selectedName
              ? `Selected: ${selectedName}`
              : hasRuntimes
                ? "Pick a runtime above."
                : "Waiting for your runtime to come online…"}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button disabled={!canConnect} onClick={onConnect}>
              Connect &amp; continue
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
          Live · Listening for your daemon
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
            Run the command above. As soon as{" "}
            <span className="font-mono">multica setup</span> finishes
            browser sign-in and the daemon starts, your runtime will
            appear here automatically (usually 10–30 seconds).
          </>
        )}
        {stage === "midway" && (
          <>
            Still listening. Make sure you finished the browser tab that{" "}
            <span className="font-mono">multica setup</span> opened — it
            needs you to approve the sign-in before the daemon can start.
          </>
        )}
        {stage === "slow" && (
          <>
            Taking longer than usual. Check the terminal where you ran{" "}
            <span className="font-mono">multica setup</span> for errors.
          </>
        )}
        {stage === "stalled" && (
          <>
            Nothing coming through yet. Close this dialog and try another
            path on the previous screen —{" "}
            <span className="font-medium text-foreground">Skip for now</span>{" "}
            (in the footer) enters your workspace in read-only mode, or
            the <span className="font-medium text-foreground">Cloud runtime</span>{" "}
            card lets you join the waitlist.
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
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>Join the cloud runtime waitlist</DialogTitle>
          <DialogDescription>
            Cloud runtimes aren&apos;t live yet. Leave your email and
            we&apos;ll email you when they are.
          </DialogDescription>
        </DialogHeader>

        <div className="pt-2">
          <CloudWaitlistExpand
            submitted={submitted}
            onSubmitted={onSubmitted}
          />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {submitted ? "Close" : "Cancel"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

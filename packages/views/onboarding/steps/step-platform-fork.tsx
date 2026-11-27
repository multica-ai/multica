"use client";

import { useEffect, useState, type ReactNode } from "react";
import {
  Check,
  CircleDashed,
  Copy,
  Download,
  Laptop,
  Link2,
  Loader2,
  Terminal,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useDaemonSetupCommands } from "@multica/core/runtimes";
import { paths } from "@multica/core/paths";
import { StepFooter, StepHeading } from "../components/step-shell";
import { useT } from "../../i18n";

/**
 * Step 3 on **web**, before any computer is connected. A browser cannot
 * install or run an AI coding tool, so this step hands off: Desktop installs
 * the tool itself, the terminal route covers headless machines, and a link
 * moves the step to the computer the member actually codes on.
 *
 * The flow swaps this for `StepAgentSetup` as soon as one of the member's
 * runtimes comes online, which is how "this page continues on its own" holds.
 */

// Single canonical download destination — the /download page owns
// OS + arch detection, the All-Platforms matrix, release-note links,
// and the CLI / Cloud alternates. Kept in sync with landing-hero.tsx
// and landing footer nav, both of which target the same path.
const DOWNLOAD_PAGE_URL = "/download";

export function StepPlatformFork({
  onSkip,
  cliInstructions,
}: {
  onSkip: () => void | Promise<void>;
  /** Platform-specific CLI install card, rendered inside the CLI dialog. */
  cliInstructions?: ReactNode;
}) {
  const { t } = useT("onboarding");
  const [cliOpen, setCliOpen] = useState(false);
  const [downloaded, setDownloaded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [skipping, setSkipping] = useState(false);

  const pickDesktop = () => {
    window.open(DOWNLOAD_PAGE_URL, "_blank", "noopener,noreferrer");
    setDownloaded(true);
  };

  const copyLink = async () => {
    // Signing in on the other computer resumes onboarding at this step.
    if (await copyText(`${window.location.origin}${paths.onboarding()}`)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleSkip = async () => {
    if (skipping) return;
    setSkipping(true);
    try {
      await onSkip();
    } finally {
      setSkipping(false);
    }
  };

  return (
    <>
      <div className="flex flex-col gap-8 pt-2 sm:pt-6">
        <StepHeading
          title={t(($) => $.step_platform.headline)}
          description={t(($) => $.step_platform.lede)}
        />

        <section className="flex flex-col gap-2">
          <SectionLabel>{t(($) => $.step_platform.recommended_label)}</SectionLabel>
          <div className="rounded-lg border border-foreground bg-card">
            <div className="flex items-center gap-3 px-4 py-3">
              <RowIcon>
                <Laptop className="h-4 w-4" />
              </RowIcon>
              <div className="min-w-0 flex-1">
                <div className="text-body font-medium text-foreground">
                  {t(($) => $.step_platform.download_title)}
                </div>
                <div className="text-caption text-muted-foreground">
                  {t(($) => $.step_platform.download_subtitle)}
                </div>
              </div>
              <Button size="sm" variant={downloaded ? "outline" : "default"} onClick={pickDesktop}>
                <Download className="h-3.5 w-3.5" />
                {downloaded
                  ? t(($) => $.step_platform.download_again)
                  : t(($) => $.step_platform.download_button)}
              </Button>
            </div>
            {downloaded && (
              <ol className="flex flex-col gap-2.5 border-t px-4 py-3 text-caption">
                <li className="flex items-start gap-2.5">
                  <Loader2 className="mt-px h-4 w-4 shrink-0 animate-spin text-foreground" />
                  <div>
                    <div className="text-foreground">{t(($) => $.step_platform.after_download_open)}</div>
                    <div className="text-muted-foreground">
                      {t(($) => $.step_platform.after_download_open_desc)}
                    </div>
                  </div>
                </li>
                <li className="flex items-start gap-2.5 text-muted-foreground">
                  <CircleDashed className="mt-px h-4 w-4 shrink-0 text-faint-foreground" />
                  {t(($) => $.step_platform.after_download_setup)}
                </li>
              </ol>
            )}
          </div>
          <p className="flex items-center gap-2 pt-1 text-caption text-muted-foreground">
            <span aria-hidden className="inline-block size-1.5 rounded-full bg-success animate-pulse" />
            {t(($) => $.step_platform.live_desktop)}
          </p>
        </section>

        <section className="flex flex-col gap-2">
          <SectionLabel>{t(($) => $.step_platform.alternatives_label)}</SectionLabel>
          <AltRow
            icon={<Terminal className="h-4 w-4" />}
            title={t(($) => $.step_platform.cli_title)}
            subtitle={t(($) => $.step_platform.cli_subtitle)}
            action={
              <Button size="sm" variant="outline" onClick={() => setCliOpen(true)}>
                {t(($) => $.step_platform.cli_action)}
              </Button>
            }
          />
          <AltRow
            icon={<Link2 className="h-4 w-4" />}
            title={t(($) => $.step_platform.handoff_title)}
            subtitle={t(($) => $.step_platform.handoff_subtitle)}
            action={
              <Button size="sm" variant="outline" onClick={copyLink}>
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copied
                  ? t(($) => $.step_platform.handoff_copied)
                  : t(($) => $.step_platform.handoff_action)}
              </Button>
            }
          />
        </section>
      </div>

      <StepFooter hint={t(($) => $.step_platform.skip_hint)}>
        <Button variant="ghost" className="w-full" disabled={skipping} onClick={handleSkip}>
          {skipping && <Loader2 className="h-4 w-4 animate-spin" />}
          {t(($) => $.step_platform.skip)}
        </Button>
      </StepFooter>

      <CliInstallDialog
        open={cliOpen}
        onClose={() => setCliOpen(false)}
        cliInstructions={cliInstructions}
      />
    </>
  );
}

function SectionLabel({ children }: { children: ReactNode }) {
  return <div className="text-caption font-medium text-muted-foreground">{children}</div>;
}

function RowIcon({ children }: { children: ReactNode }) {
  return (
    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
      {children}
    </span>
  );
}

function AltRow({
  icon,
  title,
  subtitle,
  action,
}: {
  icon: ReactNode;
  title: ReactNode;
  subtitle: ReactNode;
  action: ReactNode;
}) {
  return (
    <div className={cn("flex items-center gap-3 rounded-lg border bg-card px-4 py-3")}>
      <RowIcon>{icon}</RowIcon>
      <div className="min-w-0 flex-1">
        <div className="text-body font-medium text-foreground">{title}</div>
        <div className="text-caption text-muted-foreground">{subtitle}</div>
      </div>
      <div className="shrink-0">{action}</div>
    </div>
  );
}

// CLI install dialog

/**
 * The terminal route: the real install commands plus a live "waiting" line.
 * It has no continue button of its own — once the computer registers, the
 * flow moves the whole step on and this dialog unmounts with it.
 */
function CliInstallDialog({
  open,
  onClose,
  cliInstructions,
}: {
  open: boolean;
  onClose: () => void;
  cliInstructions?: ReactNode;
}) {
  const { t } = useT("onboarding");
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="flex max-h-[85%] flex-col sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.step_platform.cli_dialog_title)}</DialogTitle>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto pt-2">
          {cliInstructions}
          <CliWaitingStatus dialogOpen={open} />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t(($) => $.common.close)}
          </Button>
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
 * Waiting state for the CLI dialog. We can't observe the install / login /
 * daemon-start phases from the browser (they happen in the user's terminal),
 * so it confirms "we're listening" with a ticking timer and reveals
 * troubleshooting hints as time passes.
 */
function CliWaitingStatus({ dialogOpen }: { dialogOpen: boolean }) {
  const { t } = useT("onboarding");
  const commands = useDaemonSetupCommands();
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!dialogOpen || commands.status !== "ready") {
      setElapsed(0);
      return;
    }
    const id = window.setInterval(() => {
      setElapsed((e) => e + 1);
    }, 1000);
    return () => window.clearInterval(id);
  }, [dialogOpen, commands.status]);

  if (commands.status !== "ready") return null;

  // `multica setup` is ~1s config → browser sign-in (5–30s) → ~2s daemon
  // boot → register. Under 15s is normal, then "probably stuck on browser
  // auth", then "check the terminal", then point at the other routes.
  const stage: "normal" | "midway" | "slow" | "stalled" =
    elapsed < 15 ? "normal" : elapsed < 45 ? "midway" : elapsed < 90 ? "slow" : "stalled";

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-4">
      <div className="flex items-center gap-2 text-body">
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

      <p aria-live="polite" className="text-caption leading-[1.55] text-muted-foreground">
        {stage === "normal" && (
          <>
            {t(($) => $.step_platform.stage_normal_prefix)}
            <span className="font-mono break-all">{commands.setupCmd}</span>
            {t(($) => $.step_platform.stage_normal_suffix)}
          </>
        )}
        {stage === "midway" && (
          <>
            {t(($) => $.step_platform.stage_midway_prefix)}
            <span className="font-mono break-all">{commands.setupCmd}</span>
            {t(($) => $.step_platform.stage_midway_suffix)}
          </>
        )}
        {stage === "slow" && (
          <>
            {t(($) => $.step_platform.stage_slow_prefix)}
            <span className="font-mono break-all">{commands.setupCmd}</span>
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

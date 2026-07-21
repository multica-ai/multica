"use client";

import { useRef, useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, Download } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RuntimeAsidePanel } from "../components/runtime-aside-panel";
import { useT } from "../../i18n";
import { ConnectRemoteDialog } from "../../runtimes/components/connect-remote-dialog";

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
  onNext,
  onBack,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void | Promise<void>;
  onBack?: () => void;
}) {
  const { t } = useT("onboarding");
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  const [dialog, setDialog] = useState<DialogState>(null);
  const [downloaded, setDownloaded] = useState(false);

  const pickDesktop = () => {
    window.open(DOWNLOAD_PAGE_URL, "_blank", "noopener,noreferrer");
    setDownloaded(true);
  };

  const handleOpenCli = () => {
    setDialog("cli");
  };

  const handleCliConnect = (runtime: AgentRuntime) => {
    setDialog(null);
    onNext(runtime);
  };

  const footerHint = (() => {
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
                actionLabel={t(($) => $.step_platform.cloud_action)}
                disabled
              />
            </div>

            {/* Inline action bar — hint on the left, Skip on the right.
                Advancement for the CLI path is owned by the CLI
                dialog's own "Connect & continue" button; Skip creates
                the single self-serve onboarding issue. */}
            <div className="mt-8 flex max-w-[560px] flex-wrap items-center justify-between gap-x-4 gap-y-2">
              <span
                aria-live="polite"
                className="text-xs text-muted-foreground"
              >
                {footerHint}
              </span>
              <Button variant="secondary" onClick={() => onNext(null)}>
                {t(($) => $.step_runtime.skip)}
              </Button>
            </div>
          </div>
        </main>
      </div>

      {/* Right — always-visible aside */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <RuntimeAsidePanel />
        </div>
      </aside>

      {dialog === "cli" ? (
        <ConnectRemoteDialog
          workspaceId={wsId}
          onClose={() => setDialog(null)}
          onConnected={handleCliConnect}
        />
      ) : null}
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
        <div className="text-[14.5px] font-medium text-foreground">{title}</div>
        <div className="mt-1 text-[12.5px] leading-[1.5] text-muted-foreground">
          {subtitle}
        </div>
      </div>
      {disabled ? (
        <span className="shrink-0 rounded-full border bg-muted px-3 py-1 text-[12px] font-medium text-muted-foreground">
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

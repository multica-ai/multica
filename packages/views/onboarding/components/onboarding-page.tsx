"use client";

import { useRef, type CSSProperties, type ReactNode } from "react";
import { ChevronLeft } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "../../i18n";
import { OnboardingLogoutButton } from "./onboarding-logout-button";

const DRAG = { WebkitAppRegion: "drag" } as CSSProperties;
const NO_DRAG = { WebkitAppRegion: "no-drag" } as CSSProperties;

/**
 * The frame every onboarding screen renders into: one centred column under a
 * window-wide drag strip, with the wordmark and the way out in the strip's
 * band. The numbered steps share one instance, so moving between them swaps
 * only the body instead of replaying the whole window's entrance.
 *
 * The column is `min-h-full` inside the scrolling pane rather than centred by
 * it, because `items-center` on a scroll container clips the top of anything
 * taller than the window.
 */
export function OnboardingPage({
  progress,
  onBack,
  backDisabled,
  showLogout = true,
  children,
}: {
  /** Omitted on screens outside the numbered steps (welcome). */
  progress?: { step: number; total: number };
  onBack?: () => void;
  backDisabled?: boolean;
  /** First-run onboarding has no other way out; screens reached from inside
   *  the app (a second workspace, an accepted invite) leave by Back or Skip. */
  showLogout?: boolean;
  children: ReactNode;
}) {
  const { t } = useT("onboarding");
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  return (
    <div className="animate-onboarding-enter flex h-full min-h-0 flex-col bg-background">
      {/* The whole band drags the window, which is why it is one flex row
          rather than a DragStrip with controls stacked over it. The side
          columns are equal so the wordmark stays centred on the window. */}
      <header
        className="grid h-12 shrink-0 grid-cols-[1fr_auto_1fr] items-center px-3"
        style={DRAG}
      >
        <span />
        <span className="text-label font-semibold tracking-tight text-foreground">
          {t(($) => $.page.wordmark)}
        </span>
        <span className="flex justify-end" style={NO_DRAG}>
          {showLogout ? <OnboardingLogoutButton /> : null}
        </span>
      </header>

      <main
        ref={mainRef}
        style={fadeStyle}
        className="min-h-0 min-w-0 flex-1 overflow-y-auto px-6 pb-8 sm:px-10"
      >
        <div className="mx-auto flex min-h-full w-full max-w-[34rem] flex-col pt-6 sm:pt-10">
          <div className="flex h-8 items-center gap-3">
            {onBack ? (
              <Button
                variant="ghost"
                size="icon-sm"
                className="-ml-2"
                onClick={onBack}
                disabled={backDisabled}
                aria-label={t(($) => $.common.back)}
              >
                <ChevronLeft />
              </Button>
            ) : null}
            {progress ? <ProgressSegments {...progress} /> : null}
          </div>
          {children}
        </div>
      </main>
    </div>
  );
}

function ProgressSegments({ step, total }: { step: number; total: number }) {
  const { t } = useT("onboarding");
  const label = t(($) => $.page.progress, { step, total });
  return (
    <div className="flex flex-1 items-center gap-3">
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={1}
        aria-valuemax={total}
        aria-valuenow={step}
        className="flex flex-1 gap-1.5"
      >
        {Array.from({ length: total }, (_, i) => (
          <span
            key={i}
            className={cn(
              "h-1 flex-1 rounded-full transition-colors",
              i < step ? "bg-foreground" : "bg-muted",
            )}
          />
        ))}
      </div>
      <span className="shrink-0 text-caption font-medium text-muted-foreground">
        {label}
      </span>
    </div>
  );
}

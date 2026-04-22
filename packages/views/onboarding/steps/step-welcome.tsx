"use client";

import { useState } from "react";
import { ArrowRight, Download, Loader2 } from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { captureDownloadIntent } from "@multica/core/analytics";
import { cn } from "@multica/ui/lib/utils";
import { DragStrip } from "@multica/views/platform";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "../../issues/components/status-icon";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

/**
 * Step 0 — the one-shot product intro shown on every onboarding
 * entry (which-step-are-you-on is not persisted). Returning users
 * who are already onboarded never reach this screen; they're gated
 * out earlier by `!hasOnboarded`.
 *
 * Layout: two-column editorial hero on lg+, single column below.
 * Left = wordmark + serif headline + lede + CTA; right = a stack of
 * mock issue cards that show what human/agent collaboration looks
 * like on the board — the thing the user is about to create. The
 * right column is an illustration, not content: hidden below lg so
 * the headline and CTA stay the focus on narrow viewports.
 *
 * `onSkip`, when provided, renders a secondary ghost CTA that marks
 * onboarding complete server-side and sends the user straight to
 * their existing workspace. OnboardingFlow only passes it when the
 * user has ≥ 1 workspace — without that, skipping lands in limbo.
 *
 * `isWeb` flips two things when true: the subheading acknowledges
 * that web users have an extra runtime step (so "3 minutes" stops
 * being a lie), and a "Download Desktop" secondary CTA surfaces
 * before the user has invested in questionnaire / workspace. Desktop
 * bundles a daemon, so the same prompt would be noise there.
 */
export function StepWelcome({
  onNext,
  onSkip,
  isWeb = false,
}: {
  onNext: () => void | Promise<void>;
  onSkip?: () => void | Promise<void>;
  isWeb?: boolean;
}) {
  // Tracks which button is mid-flight so we can show a per-button
  // spinner and disable both while one is in progress.
  const [pending, setPending] = useState<"next" | "skip" | null>(null);

  const handleNext = async () => {
    if (pending) return;
    setPending("next");
    try {
      await onNext();
    } finally {
      setPending(null);
    }
  };

  const handleSkip = async () => {
    if (pending || !onSkip) return;
    setPending("skip");
    try {
      await onSkip();
    } finally {
      setPending(null);
    }
  };

  return (
    <div className="animate-onboarding-enter flex h-full min-h-[640px] flex-col lg:flex-row">
      {/* Left — prose + CTA */}
      <div className="flex flex-col lg:flex-1">
        <DragStrip />
        <div className="flex flex-1 flex-col justify-center px-6 pb-12 sm:px-10 md:px-20 lg:px-20 xl:px-24">
          <div className="flex w-full max-w-[540px] flex-col gap-8">
            <div className="flex items-center gap-2.5">
              <MulticaIcon className="size-5 text-foreground" noSpin />
              <span className="font-serif text-xl font-medium tracking-tight">
                Welcome to Multica
              </span>
            </div>

            <h1 className="text-balance font-serif text-5xl font-medium leading-[1.04] tracking-tight sm:text-6xl">
              Your AI teammates,
              <br />
              in <em className="italic text-brand">one workspace.</em>
            </h1>

            <div className="flex flex-col gap-4">
              <p className="text-lg leading-relaxed text-foreground/85">
                Assign them work like you&apos;d assign a colleague — they
                pick it up, update status, and comment when done.
              </p>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {isWeb ? (
                  <>
                    Desktop bundles the runtime — nothing to install.
                    Continue on web to connect your own CLI.
                  </>
                ) : (
                  "By the end, a real agent will be replying to your first issue."
                )}
              </p>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              {isWeb ? (
                <>
                  {/* `<a>` rather than `<Button onClick={window.open}>`
                      so middle-click / cmd-click / "Copy link" all
                      behave and screen readers announce it as a link
                      (it navigates; `Continue on web` is the button
                      that mutates flow state). New tab preserves this
                      onboarding tab in case the desktop install
                      stalls and the user falls back here. */}
                  <a
                    href="/download"
                    target="_blank"
                    rel="noopener noreferrer"
                    onClick={() => captureDownloadIntent("welcome")}
                    className={buttonVariants({ size: "lg" })}
                  >
                    <Download className="h-4 w-4" />
                    Download Desktop
                  </a>
                  <Button
                    size="lg"
                    variant="outline"
                    onClick={handleNext}
                    disabled={pending !== null}
                  >
                    {pending === "next" && (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    )}
                    Continue on web
                    <ArrowRight className="h-4 w-4" />
                  </Button>
                </>
              ) : (
                <Button
                  size="lg"
                  onClick={handleNext}
                  disabled={pending !== null}
                >
                  {pending === "next" && (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  )}
                  Start exploring
                  <ArrowRight className="h-4 w-4" />
                </Button>
              )}
              {onSkip && (
                <Button
                  size="lg"
                  variant="ghost"
                  onClick={handleSkip}
                  disabled={pending !== null}
                >
                  {pending === "skip" && (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  )}
                  I&apos;ve done this before
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Right — mock issue cards illustration. Hidden on < lg.
          Flex row on lg+ with `items-stretch` (default) makes both
          columns take the container's full height, so the muted bg
          fills the viewport edge-to-edge. `justify-center` inside
          centers the mock cards vertically, mirroring the left
          column's copy-center layout. */}
      <div className="hidden border-l bg-muted/40 lg:flex lg:flex-1 lg:flex-col lg:overflow-hidden">
        <DragStrip />
        <div className="flex flex-1 flex-col items-center justify-center gap-7 px-8 py-8">
          <p className="max-w-[440px] text-balance text-center font-serif text-[15px] italic leading-snug text-muted-foreground">
            Every issue, every thread, every decision — shared by your team and
            agents.
          </p>
          <WelcomeIllustration />
        </div>
      </div>
    </div>
  );
}


/**
 * A day in a solo user's multi-agent workspace. Five activity cards
 * woven through 3 shared issues (MCA-42 appears 3×) so the reader can
 * *see* agents referencing each other's work — the product's
 * "one workspace, shared context" thesis rendered concretely.
 *
 * Cards use slight rotations + indents to feel like a hand-stacked
 * pile rather than a neat feed, which matches the editorial-hero
 * aesthetic of the left column.
 */
function WelcomeIllustration() {
  return (
    <div className="flex w-full max-w-[460px] flex-col gap-3">
      <MockActivityCard
        actor={{ kind: "user", name: "You", initial: "N" }}
        issueId="MCA-42"
        content={
          <>
            <Mention>@Content Agent</Mention> can you draft a short launch
            post? Pull from <Mention>@Research Agent</Mention>&apos;s interview
            findings.
          </>
        }
      />
      <MockActivityCard
        className="-translate-x-5 -rotate-[1.2deg]"
        actor={{ kind: "agent", name: "Content Agent", provider: "codex" }}
        issueId="MCA-42"
        content={
          <>
            On it. Pulling Research&apos;s quotes, drafting around the
            &ldquo;time saved&rdquo; angle…
          </>
        }
        status="in_progress"
      />
      <MockActivityCard
        className="translate-x-8 rotate-[1.6deg]"
        actor={{ kind: "agent", name: "Research Agent", provider: "hermes" }}
        issueId="MCA-38"
        content="This week's user interviews summarized — 12 calls, 4 recurring themes, 3 pull-quotes."
        status="done"
        timestamp="15 min ago"
      />
      <MockActivityCard
        className="-translate-x-6 -rotate-[0.8deg]"
        actor={{ kind: "agent", name: "Review Agent", provider: "openclaw" }}
        issueId="MCA-42"
        content="Reviewed Monday's draft — left 4 notes on tone. Standing by for the new one."
        status="in_review"
      />
      <MockActivityCard
        className="translate-x-6 rotate-[1deg]"
        actor={{ kind: "agent", name: "Coding Agent", provider: "claude" }}
        issueId="MCA-35"
        content={
          <>
            Shipped the export feature <Mention>@you</Mention> flagged.
            Preview link in the PR.
          </>
        }
        status="done"
        timestamp="just now"
      />
    </div>
  );
}

type ProviderName =
  | "claude"
  | "codex"
  | "opencode"
  | "openclaw"
  | "hermes"
  | "pi"
  | "copilot"
  | "cursor";

type ActivityActor =
  | { kind: "user"; name: string; initial: string }
  | { kind: "agent"; name: string; provider: ProviderName };

function MockActivityCard({
  actor,
  issueId,
  content,
  status,
  timestamp,
  className,
}: {
  actor: ActivityActor;
  issueId: string;
  content: React.ReactNode;
  status?: Extract<IssueStatus, "in_progress" | "done" | "in_review">;
  timestamp?: string;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "rounded-lg border bg-card px-4 py-3.5 shadow-sm",
        // Decorative hover: lift, straighten, deeper shadow. Cards aren't
        // clickable — this is ambient polish so the illustration feels like
        // real app UI rather than a flat screenshot.
        "transition-all duration-200 ease-out will-change-transform",
        "hover:-translate-y-0.5 hover:rotate-0 hover:shadow-md",
        className,
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <MockAvatar actor={actor} />
          <span className="truncate text-sm font-medium text-foreground">
            {actor.name}
          </span>
        </div>
        <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
          {issueId}
        </span>
      </div>

      <p className="mt-2.5 text-sm leading-snug text-foreground/85">
        {content}
      </p>

      {status && <StatusFooter status={status} timestamp={timestamp} />}
    </div>
  );
}

function MockAvatar({ actor }: { actor: ActivityActor }) {
  if (actor.kind === "user") {
    return (
      <div
        aria-hidden
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-foreground text-[11px] font-semibold text-background"
      >
        {actor.initial}
      </div>
    );
  }
  return (
    <div
      aria-hidden
      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border bg-muted/40 text-foreground"
    >
      <ProviderLogo provider={actor.provider} className="h-3.5 w-3.5" />
    </div>
  );
}

function StatusFooter({
  status,
  timestamp,
}: {
  status: IssueStatus;
  timestamp?: string;
}) {
  const cfg = STATUS_CONFIG[status];
  return (
    <div className="mt-3 flex items-center gap-2 text-xs">
      <span
        className={cn("flex items-center gap-1.5 font-medium", cfg.iconColor)}
      >
        <StatusIcon
          status={status}
          className={cn(
            "h-3.5 w-3.5",
            status === "in_progress" && "animate-pulse",
          )}
        />
        {cfg.label}
      </span>
      {timestamp && (
        <>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">{timestamp}</span>
        </>
      )}
    </div>
  );
}

function Mention({ children }: { children: React.ReactNode }) {
  return <span className="font-medium text-brand">{children}</span>;
}

"use client";

import { useState } from "react";
import { Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useLocale, useT } from "../../i18n";
import { formatAgentNames } from "../../issues/components/steer-receipts";
import { StepFooter } from "../components/step-shell";
import { toolDisplayName } from "../provisioning";

/**
 * What onboarding is about to do, in the order it will do it, plus the coding
 * tools the desktop daemon already found here — the one fact that decides
 * whether the next step is "pick one" or "install one".
 */
export function StepWelcome({
  onStart,
  onSkip,
  isWeb = false,
  detectedProviders = [],
}: {
  onStart: () => Promise<void>;
  /** Shown only to someone who already has a workspace. */
  onSkip?: () => Promise<void>;
  isWeb?: boolean;
  /** Provider slugs the local daemon found on PATH (desktop). */
  detectedProviders?: string[];
}) {
  const { t } = useT("onboarding");
  const locale = useLocale();
  const [pending, setPending] = useState<"start" | "skip" | null>(null);

  const run = async (which: "start" | "skip", action: () => Promise<void>) => {
    if (pending) return;
    setPending(which);
    try {
      await action();
    } finally {
      setPending(null);
    }
  };

  const plan = isWeb
    ? [
        [t(($) => $.welcome.plan_connect_title), t(($) => $.welcome.plan_connect_body)],
        [t(($) => $.welcome.plan_tool_title), t(($) => $.welcome.plan_tool_body)],
        [t(($) => $.welcome.plan_task_title), t(($) => $.welcome.plan_task_body)],
      ]
    : [
        [t(($) => $.welcome.plan_tool_title), t(($) => $.welcome.plan_tool_body)],
        [t(($) => $.welcome.plan_task_title), t(($) => $.welcome.plan_task_body)],
        [t(($) => $.welcome.plan_watch_title), t(($) => $.welcome.plan_watch_body)],
      ];

  const found = detectedProviders.map(toolDisplayName);

  return (
    <>
      <div className="flex flex-col gap-3 pt-6">
        <h1 className="text-balance text-title-lg font-semibold tracking-tight text-foreground sm:text-display-sm">
          {t(($) => $.welcome.headline)}
        </h1>
        <p className="text-pretty text-body text-muted-foreground">
          {t(($) => $.welcome.lede)}
        </p>
      </div>

      <ol className="mt-8 flex flex-col gap-4">
        {plan.map(([title, body], i) => (
          <li key={title} className="flex gap-3">
            <span className="flex size-6 shrink-0 items-center justify-center rounded-full border text-caption font-medium text-muted-foreground">
              {i + 1}
            </span>
            <div className="min-w-0">
              <div className="text-body font-medium text-foreground">{title}</div>
              <div className="text-caption text-muted-foreground">{body}</div>
            </div>
          </li>
        ))}
      </ol>

      {!isWeb ? (
        <div className="mt-8 flex items-center gap-3 rounded-lg bg-muted/60 px-4 py-3">
          {found.length > 0 ? (
            <>
              <span className="flex shrink-0 -space-x-1">
                {detectedProviders.slice(0, 3).map((p) => (
                  <span
                    key={p}
                    className="flex size-6 items-center justify-center rounded-md border bg-card"
                  >
                    <ProviderLogo provider={p} className="size-3.5" />
                  </span>
                ))}
              </span>
              <span className="min-w-0 text-caption text-foreground">
                {t(($) => $.welcome.detected, { tools: formatAgentNames(locale, found) })}
              </span>
            </>
          ) : (
            <span className="text-caption text-muted-foreground">
              {t(($) => $.welcome.detected_none)}
            </span>
          )}
        </div>
      ) : null}

      <StepFooter>
        <Button
          size="lg"
          className="w-full"
          onClick={() => run("start", onStart)}
          disabled={pending !== null}
          aria-busy={pending === "start"}
        >
          {pending === "start" ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.welcome.start)}
        </Button>
        {onSkip ? (
          <Button
            variant="ghost"
            className="w-full text-muted-foreground"
            onClick={() => run("skip", onSkip)}
            disabled={pending !== null}
            aria-busy={pending === "skip"}
          >
            {pending === "skip" ? <Loader2 className="animate-spin" /> : null}
            {t(($) => $.welcome.skip_existing)}
          </Button>
        ) : null}
      </StepFooter>
    </>
  );
}

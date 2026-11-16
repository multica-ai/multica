"use client";

import { CheckCircle2, Circle } from "@multica/ui/icons";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";

export type AgentSetupStepId = "runtime" | "agent" | "issue";

export interface AgentSetupStep {
  id: AgentSetupStepId;
  done: boolean;
  /** The first step not yet done; the one the user should do now. */
  current: boolean;
}

/** An agent needs a runtime to run on, then an issue to work on. */
export function agentSetupSteps({
  hasRuntime,
  hasAgent,
  hasAssignedIssue,
}: {
  hasRuntime: boolean;
  hasAgent: boolean;
  hasAssignedIssue: boolean;
}): AgentSetupStep[] {
  const done: Record<AgentSetupStepId, boolean> = {
    runtime: hasRuntime,
    agent: hasAgent,
    issue: hasAssignedIssue,
  };
  const order: AgentSetupStepId[] = ["runtime", "agent", "issue"];
  const current = order.find((id) => !done[id]);
  return order.map((id) => ({ id, done: done[id], current: id === current }));
}

export function AgentSetupChecklist({
  hasRuntime,
  runtimesHref,
}: {
  hasRuntime: boolean;
  runtimesHref: string;
}) {
  const { t } = useT("agents");
  const steps = agentSetupSteps({ hasRuntime, hasAgent: false, hasAssignedIssue: false });
  const label: Record<AgentSetupStepId, string> = {
    runtime: t(($) => $.empty.setup.runtime),
    agent: t(($) => $.empty.setup.agent),
    issue: t(($) => $.empty.setup.issue),
  };

  return (
    <ol
      aria-label={t(($) => $.empty.setup.label)}
      className="mt-2 w-full max-w-sm divide-y rounded-lg border bg-card text-left"
    >
      {steps.map((step) => (
        <li key={step.id} className="flex min-h-11 items-center gap-2.5 px-3 py-2 text-label">
          {step.done ? (
            <CheckCircle2 aria-hidden="true" className="size-4 shrink-0 text-success" />
          ) : (
            <Circle aria-hidden="true" className="size-4 shrink-0 text-faint-foreground" />
          )}
          <span
            className={cn(
              "min-w-0 flex-1",
              step.done ? "text-muted-foreground line-through" : "text-foreground",
              step.current && "font-medium",
            )}
          >
            {label[step.id]}
            {step.done ? <span className="sr-only">{t(($) => $.empty.setup.done)}</span> : null}
          </span>
          {step.id === "runtime" && !step.done ? (
            <AppLink
              href={runtimesHref}
              className={buttonVariants({ variant: "ghost", size: "xs" })}
            >
              {t(($) => $.empty.setup.connect_runtime)}
            </AppLink>
          ) : null}
        </li>
      ))}
    </ol>
  );
}

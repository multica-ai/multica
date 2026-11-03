"use client";

import type { ReactNode } from "react";
import { Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { AGENT_CREATE_STEP_COUNT, type AgentCreateStep } from "./create-shell";

/**
 * The bar pinned under every creation step: where the reader is on the left,
 * the way out and the one way forward on the right. Its end padding keeps the
 * forward button clear of the chat launcher, which owns that corner.
 */
export function CreateFlowFooter({
  stepIndex,
  status,
  children,
}: {
  /** Position in runtime → configure → done. Omitted by off-path routes. */
  stepIndex?: AgentCreateStep;
  /** Beside the step: the current selection, or what went wrong. */
  status?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useT("agents");
  return (
    <footer className="pe-chat-launcher flex min-h-14 shrink-0 items-center gap-3 border-t bg-background py-3 pl-4">
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {stepIndex ? (
          <p className="shrink-0 text-caption text-muted-foreground">
            {t(($) => $.creation_studio.steps.progress, {
              current: stepIndex,
              total: AGENT_CREATE_STEP_COUNT,
            })}
          </p>
        ) : null}
        {status}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </footer>
  );
}

/**
 * Where the configure step ends. Both exits live here — commit the agent, or
 * leave — because they are the two answers to one question, and reading them
 * side by side is what makes the destructive one legible. A discard control
 * parked elsewhere (say, a trash icon over the transcript) reads as "clear this
 * chat" rather than "throw away what I am building".
 *
 * The builder passes `onDiscard`: its conversation outlives the screen, so
 * leaving it behind has to be an explicit choice. The manual route passes
 * `onCancel` instead; its form has nothing to throw away.
 */
export function CreateAgentFooter({
  stepIndex,
  canCreate,
  creating,
  squad,
  error,
  onCreate,
  onCancel,
  onDiscard,
  discarding = false,
}: {
  stepIndex?: AgentCreateStep;
  canCreate: boolean;
  creating: boolean;
  squad: boolean;
  error: string | null;
  onCreate: () => void;
  onCancel?: () => void;
  onDiscard?: () => void;
  discarding?: boolean;
}) {
  const { t } = useT("agents");
  return (
    <CreateFlowFooter
      stepIndex={stepIndex}
      status={
        error ? (
          <p role="alert" className="min-w-0 flex-1 break-words text-body text-destructive">
            {error}
          </p>
        ) : null
      }
    >
      {onDiscard ? (
        <Button
          type="button"
          variant="ghost"
          className="text-muted-foreground hover:text-foreground"
          onClick={onDiscard}
          disabled={creating || discarding}
        >
          {t(($) => $.creation_studio.drafts.discard)}
        </Button>
      ) : onCancel ? (
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t(($) => $.creation_studio.runtime_step.cancel)}
        </Button>
      ) : null}
      <Button type="button" onClick={onCreate} disabled={!canCreate}>
        {creating && <Loader2 className="size-4 animate-spin" />}
        {creating
          ? t(($) => $.creation_studio.creating)
          : squad
            ? t(($) => $.creation_studio.create_and_add)
            : t(($) => $.creation_studio.create_agent)}
      </Button>
    </CreateFlowFooter>
  );
}

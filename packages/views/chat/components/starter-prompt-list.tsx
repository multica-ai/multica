"use client";

import type { AgentStarterPrompt } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * The generic suggestions a new chat shows for an agent whose owner
 * configured none. This is the ONLY definition of them: the settings preview
 * renders the same hook, so an author configuring an agent sees exactly what
 * a real new chat will show rather than a hand-maintained mock of it.
 */
export function useFallbackStarterPrompts(): AgentStarterPrompt[] {
  const { t } = useT("chat");
  return [
    {
      label: t(($) => $.starter_prompts.capabilities.label),
      prompt: t(($) => $.starter_prompts.capabilities.prompt),
    },
    {
      label: t(($) => $.starter_prompts.first_task.label),
      prompt: t(($) => $.starter_prompts.first_task.prompt),
    },
    {
      label: t(($) => $.starter_prompts.recommend.label),
      prompt: t(($) => $.starter_prompts.recommend.prompt),
    },
  ];
}

const ROW_CLASS =
  "w-full rounded-lg border border-border bg-card px-3 py-2.5 text-left text-body text-foreground";

/**
 * The stack of starter buttons in a chat's empty state.
 *
 * Omit `onPick` to render the same rows as inert presentation — that is the
 * settings preview, which must look like the real thing without offering a
 * second, non-functional way to "send" a prompt from a settings page.
 */
export function StarterPromptList({
  prompts,
  onPick,
  className,
}: {
  prompts: AgentStarterPrompt[];
  onPick?: (prompt: string) => void;
  className?: string;
}) {
  if (!onPick) {
    return (
      <div className={cn("w-full space-y-2", className)} aria-hidden="true">
        {prompts.map((item, index) => (
          <div key={index} className={cn(ROW_CLASS, "truncate")}>
            {item.label}
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className={cn("w-full space-y-2", className)}>
      {prompts.map((item, index) => (
        <button
          key={index}
          type="button"
          onClick={() => onPick(item.prompt)}
          className={cn(
            ROW_CLASS,
            "transition-colors hover:border-brand/40 hover:bg-accent",
          )}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

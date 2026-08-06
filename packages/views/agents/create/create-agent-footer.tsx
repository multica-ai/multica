"use client";

import { Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Where the flow ends. Both exits live here — commit the agent, or abandon the
 * attempt — because they are the two answers to one question, and reading them
 * side by side is what makes the destructive one legible. A discard control
 * parked elsewhere (say, a trash icon over the transcript) reads as "clear this
 * chat" rather than "throw away what I am building".
 *
 * `onDiscard` is optional: the manual route has nothing to abandon, its form is
 * gone the moment the user navigates away.
 */
export function CreateAgentFooter({
  canCreate,
  creating,
  squad,
  error,
  onCreate,
  onDiscard,
  discarding = false,
}: {
  canCreate: boolean;
  creating: boolean;
  squad: boolean;
  error: string | null;
  onCreate: () => void;
  onDiscard?: () => void;
  discarding?: boolean;
}) {
  const { t } = useT("agents");
  return (
    <div className="pe-chat-launcher sticky bottom-0 mt-8 flex items-center justify-between gap-3 border-t bg-background/95 py-3 pl-5 backdrop-blur">
      {error ? (
        <p
          role="alert"
          className="min-w-0 flex-1 break-words text-body text-destructive"
        >
          {error}
        </p>
      ) : null}
      {onDiscard ? (
        <Button
          type="button"
          variant="ghost"
          className="ml-auto shrink-0 text-muted-foreground hover:text-foreground"
          onClick={onDiscard}
          disabled={creating || discarding}
        >
          {t(($) => $.creation_studio.drafts.discard)}
        </Button>
      ) : null}
      <Button
        type="button"
        className={cn("shrink-0", !onDiscard && "ml-auto")}
        onClick={onCreate}
        disabled={!canCreate}
      >
        {creating && <Loader2 className="size-4 animate-spin" />}
        {creating
          ? t(($) => $.creation_studio.creating)
          : squad
            ? t(($) => $.creation_studio.create_and_add)
            : t(($) => $.creation_studio.create_and_open)}
      </Button>
    </div>
  );
}

"use client";

import { Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/** Where the manual creation flow commits the agent. */
export function CreateAgentFooter({
  canCreate,
  creating,
  squad,
  error,
  onCreate,
}: {
  canCreate: boolean;
  creating: boolean;
  squad: boolean;
  error: string | null;
  onCreate: () => void;
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
      <Button
        type="button"
        className="ml-auto shrink-0"
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

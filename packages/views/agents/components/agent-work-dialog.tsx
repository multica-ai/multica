"use client";

import type { Agent } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { useT } from "../../i18n";

/** The agent's issues, with the issue list's own scopes, filters and display
 *  options. */
export function AgentWorkDialog({
  agent,
  open,
  onOpenChange,
}: {
  agent: Agent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("agents");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(85dvh,52rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="shrink-0 gap-1 border-b px-5 py-4 pr-12">
          <DialogTitle>{t(($) => $.tabs.work)}</DialogTitle>
          <DialogDescription className="truncate">{agent.name}</DialogDescription>
        </DialogHeader>
        <div className="relative flex min-h-0 flex-1 flex-col">
          <ActorIssuesPanel actorType="agent" actorId={agent.id} />
        </div>
      </DialogContent>
    </Dialog>
  );
}

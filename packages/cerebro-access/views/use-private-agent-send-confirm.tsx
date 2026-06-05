"use client";

// FIR-32: a one-step confirm shown when the user is about to send a comment
// that would wake an agent that is neither their own nor a workspace agent.
// The confirm only GATES the send — the existing FIR-2385 run-request-to-owner
// flow is untouched. On "Send request" the comment is posted as usual and the
// backend routes the approval to the agent's owner; on "Cancel" the draft stays.

import { useCallback, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { getCurrentWsId } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { pickAgentNeedingConfirm } from "./private-agent-confirm";

interface PendingConfirm {
  agentName: string;
  ownerName: string | null;
  resolve: (proceed: boolean) => void;
}

export interface PrivateAgentSendConfirm {
  /**
   * Call from the composer's submit handler with the draft markdown. Resolves
   * `true` when the send may proceed, `false` when the user cancels. When no
   * confirm is needed it resolves `true` synchronously, so it is safe to await
   * on every send.
   */
  confirmBeforeSend: (markdown: string) => Promise<boolean>;
  /** Render once inside the composer — the dialog portals itself. */
  confirmDialog: ReactNode;
}

/**
 * usePrivateAgentSendConfirm — wraps a composer's send with a confirm when the
 * resolved trigger target is a private agent the user does not own. Gated by
 * the same flag as the run-request feature it explains (`cerebro_private_agent_requests`).
 */
export function usePrivateAgentSendConfirm({
  triggerAgentId,
}: {
  triggerAgentId?: string;
}): PrivateAgentSendConfirm {
  const flagOn = useFeatureFlag("cerebro_private_agent_requests");
  const wsId = getCurrentWsId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const pendingRef = useRef<PendingConfirm | null>(null);

  const settle = useCallback((proceed: boolean) => {
    const current = pendingRef.current;
    pendingRef.current = null;
    setPending(null);
    current?.resolve(proceed);
  }, []);

  const confirmBeforeSend = useCallback(
    (markdown: string): Promise<boolean> => {
      if (!flagOn) return Promise.resolve(true);
      const role = members.find((m) => m.user_id === userId)?.role ?? null;
      const agent = pickAgentNeedingConfirm({
        markdown,
        triggerAgentId,
        agents,
        userId,
        role,
      });
      if (!agent) return Promise.resolve(true);
      const ownerName = agent.owner_id
        ? members.find((m) => m.user_id === agent.owner_id)?.name ?? null
        : null;
      return new Promise<boolean>((resolve) => {
        const next = { agentName: agent.name, ownerName, resolve };
        pendingRef.current = next;
        setPending(next);
      });
    },
    [flagOn, agents, members, userId, triggerAgentId],
  );

  const owner = pending?.ownerName;
  const agentName = pending?.agentName ?? "This agent";
  const confirmDialog = (
    <AlertDialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (!open) settle(false);
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <Bot />
          </AlertDialogMedia>
          <AlertDialogTitle>Start an agent?</AlertDialogTitle>
          <AlertDialogDescription>
            {`You haven't tagged a person, so this starts ${agentName}` +
              (owner ? `, a personal agent owned by ${owner}. ` : ", a personal agent. ") +
              `Sending asks ${owner ?? "its owner"} to approve — it only runs once they do.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => settle(false)}>
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction onClick={() => settle(true)}>
            Send request
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { confirmBeforeSend, confirmDialog };
}

"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { DRAFT_NEW_SESSION, useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { canAssignAgent } from "../issues/components/pickers/assignee-picker";
import { useNavigation } from "../navigation";

/**
 * Opens a new chat with `draft` already in the composer, addressed to the
 * agent the user last chatted with (or the first one they may use). Returns
 * null while no agent is available, so callers can hide the entry point.
 */
export function useStartChatWithDraft(): ((draft: string) => void) | null {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const paths = useWorkspacePaths();
  const { push } = useNavigation();

  const role = members.find((m) => m.user_id === userId)?.role;
  const available = agents.filter((a) => !a.archived_at && canAssignAgent(a, userId, role));
  const agentId = (available.find((a) => a.id === selectedAgentId) ?? available[0])?.id;

  const start = useCallback(
    (draft: string) => {
      if (!agentId) return;
      // The `?agent=` deep link starts a fresh compose, which keeps the
      // new-chat draft slot, so the text lands in the composer it opens.
      useChatStore.getState().setInputDraft(DRAFT_NEW_SESSION, draft);
      push(`${paths.chat()}?agent=${encodeURIComponent(agentId)}`);
    },
    [agentId, paths, push],
  );

  return agentId ? start : null;
}

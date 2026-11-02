"use client";

import { useCallback } from "react";
import { DRAFT_NEW_SESSION, useChatStore } from "@multica/core/chat";
import type { KnowledgeHit } from "@multica/core/knowledge";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { isWebLink } from "./web-link";

/** How many top results an Ask the Secretary prompt carries. */
export const ASK_SECRETARY_RESULT_LIMIT = 3;

/** The longest passage text one result carries into the prompt. */
const PASSAGE_LIMIT = 600;

/**
 * One numbered result as the prompt shows it: the title and base, the source
 * link when the document has a web address, then the passage. The Secretary
 * cites by title and link, so both are kept whole.
 */
export function formatSecretaryResult(
  hit: KnowledgeHit,
  index: number,
  knowledgeBaseName: string,
): string {
  const link = isWebLink(hit.source) ? ` - ${hit.source}` : "";
  const passage = hit.content.trim();
  const text =
    passage.length > PASSAGE_LIMIT ? `${passage.slice(0, PASSAGE_LIMIT)}...` : passage;
  return `${index + 1}. ${hit.title} (${knowledgeBaseName})${link}\n${text}`;
}

/**
 * Opens the Secretary's chat with the prompt in the composer, unsent, so the
 * boss reads it before it goes. With the floating chat on it opens that
 * window; with the preference off, the Chat tab opens on the Secretary.
 */
export function useAskSecretary(): (agentId: string, prompt: string) => void {
  const navigation = useNavigation();
  const workspaceSlug = useWorkspaceSlug();

  return useCallback(
    (agentId: string, prompt: string) => {
      const chat = useChatStore.getState();
      chat.setSelectedAgentId(agentId);
      chat.setSelectedProjectId(null);
      chat.setActiveSession(null);
      chat.setInputDraft(DRAFT_NEW_SESSION, prompt);
      if (chat.floatingChatEnabled) {
        chat.setOpen(true);
        return;
      }
      if (workspaceSlug) {
        navigation.push(paths.workspace(workspaceSlug).chatWithAgent(agentId));
      }
    },
    [navigation, workspaceSlug],
  );
}

import { DRAFT_NEW_SESSION, useChatStore } from "../chat";

/**
 * Starts a new chat with the Secretary and puts `text` in its composer. The
 * text is appended, not replacing: a half-written message the viewer already
 * has in the new-chat composer is kept above it. Opening the chat window or
 * navigating to Chat is the caller's to do.
 */
export function prefillSecretaryChat(secretaryId: string, text: string): void {
  const chat = useChatStore.getState();
  chat.setSelectedAgentId(secretaryId);
  chat.setActiveSession(null);
  chat.appendToInputDraft(DRAFT_NEW_SESSION, text);
}

import type { QueryClient } from "@tanstack/react-query";
import type { Issue, UpdateIssueRequest } from "../types";

export interface DescriptionDraft {
  markdown: string;
  baseMarkdown: string;
  attachmentIds: string[];
}

type Save = (input: UpdateIssueRequest & { id: string }) => Promise<Issue>;
type PendingSave = {
  draft: DescriptionDraft;
  save: Save;
  onError: (error: unknown) => void;
};
type DescriptionQueue = { pending: PendingSave | null };
type DescriptionSaveSession = {
  active: boolean;
  queues: Map<string, DescriptionQueue>;
};

// The QueryClient can survive logout. Session cleanup replaces this owner so
// neither a pending request nor an old editor's unmount flush can join a new login.
const sessions = new WeakMap<QueryClient, DescriptionSaveSession>();

export function getIssueDescriptionSaveSession(client: QueryClient) {
  let session = sessions.get(client);
  if (!session) {
    session = { active: true, queues: new Map() };
    sessions.set(client, session);
  }
  return session;
}

export function clearIssueDescriptionSaves(client: QueryClient): void {
  const session = sessions.get(client);
  if (!session) return;
  session.active = false;
  for (const queue of session.queues.values()) queue.pending = null;
  session.queues.clear();
  sessions.delete(client);
}

export function queueIssueDescriptionSave(
  client: QueryClient,
  workspaceId: string,
  issueId: string,
  draft: DescriptionDraft,
  save: Save,
  onError: (error: unknown) => void,
  session = getIssueDescriptionSaveSession(client),
): void {
  if (!session.active) return;
  const active = session.queues;
  const key = `${workspaceId}:${issueId}`;
  const pending = { draft, save, onError };
  const existing = active.get(key);
  if (existing) {
    existing.pending = pending;
    return;
  }
  const queue: DescriptionQueue = { pending };
  active.set(key, queue);

  void (async () => {
    let submittedBase: string | undefined;
    try {
      while (session.active && queue.pending) {
        const next = queue.pending;
        queue.pending = null;
        try {
          await next.save({
            id: issueId,
            description: next.draft.markdown,
            description_base: submittedBase ?? next.draft.baseMarkdown,
            attachment_ids: next.draft.attachmentIds.length > 0
              ? next.draft.attachmentIds
              : undefined,
          });
        } catch (error) {
          // Preserve the existing failure contract: release the gate and let
          // the next user edit try again, without replaying a failed queue.
          if (session.active) next.onError(error);
          break;
        }
        // Do not adopt server-appended channel media the editor never saw.
        // The submitted text lets the server preserve that media next time.
        submittedBase = next.draft.markdown;
      }
    } finally {
      if (active.get(key) === queue) active.delete(key);
    }
  })();
}

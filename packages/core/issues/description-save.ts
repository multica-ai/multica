import { useCallback } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "../hooks";
import type { Issue, UpdateIssueRequest } from "../types";
import { useUpdateIssue } from "./mutations";

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

// Only active saves live here. A remounted editor joins the same issue's
// queue, so its newer draft replaces any unsent draft from the closed editor.
// QueryClient and workspace identity keep separate sessions isolated.
const queues = new WeakMap<QueryClient, Map<string, DescriptionQueue>>();

export function queueIssueDescriptionSave(
  client: QueryClient,
  workspaceId: string,
  issueId: string,
  draft: DescriptionDraft,
  save: Save,
  onError: (error: unknown) => void,
): void {
  let active = queues.get(client);
  if (!active) {
    active = new Map();
    queues.set(client, active);
  }
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
      while (queue.pending) {
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
          next.onError(error);
          break;
        }
        // Do not adopt server-appended channel media the editor never saw.
        // The submitted text lets the server preserve that media next time.
        submittedBase = next.draft.markdown;
      }
    } finally {
      active.delete(key);
      if (active.size === 0) queues.delete(client);
    }
  })();
}

export function useIssueDescriptionSave(issueId: string, onError: (error: unknown) => void) {
  const client = useQueryClient();
  const workspaceId = useWorkspaceId();
  const { mutateAsync } = useUpdateIssue();
  return useCallback((draft: DescriptionDraft) => {
    // mutateAsync settles even after the mutation observer's last listener
    // unmounts; per-call mutate callbacks do not.
    queueIssueDescriptionSave(client, workspaceId, issueId, draft, mutateAsync, onError);
  }, [client, workspaceId, issueId, mutateAsync, onError]);
}

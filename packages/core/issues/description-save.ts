import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "../hooks";
import { useUpdateIssue } from "./mutations";
import {
  getIssueDescriptionSaveSession,
  queueIssueDescriptionSave,
  type DescriptionDraft,
} from "./description-save-queue";

export { queueIssueDescriptionSave, type DescriptionDraft } from "./description-save-queue";

export function useIssueDescriptionSave(issueId: string, onError: (error: unknown) => void) {
  const client = useQueryClient();
  const workspaceId = useWorkspaceId();
  const session = getIssueDescriptionSaveSession(client);
  const { mutateAsync } = useUpdateIssue();
  return useCallback((draft: DescriptionDraft) => {
    // mutateAsync settles even after the mutation observer's last listener
    // unmounts; per-call mutate callbacks do not.
    queueIssueDescriptionSave(client, workspaceId, issueId, draft, mutateAsync, onError, session);
  }, [client, workspaceId, issueId, mutateAsync, onError, session]);
}

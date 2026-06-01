/**
 * Mobile reminder mutations (FIR-2644).
 *
 * `useCreateCommentReminder` hangs a personal reminder on one comment via the
 * cerebro backend (POST /api/inbox/reminders with comment_id). The created
 * reminder lands as a muted inbox row that surfaces at planned_at, so we
 * invalidate the inbox list on settle to pull it in — mirrors web's
 * packages/core/inbox/mutations.ts create-reminder path.
 *
 * No optimistic patch: the row is muted-until-future, so it should NOT appear
 * in the active inbox immediately, and the server assigns its id/shape. An
 * optimistic insert would briefly show a reminder that isn't due yet.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { inboxKeys } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreateCommentReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (params: {
      commentId: string;
      issueId: string;
      text: string;
      plannedAt: Date;
    }) => api.createCommentReminder(params),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

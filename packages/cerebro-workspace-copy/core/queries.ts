import { useMutation } from "@tanstack/react-query";
import { copyEntity, relinkIssues } from "./api";
import { ISSUE_SHAPED, type CopyResult, type CopyToWorkspaceInput } from "./types";

// useCopyToWorkspace copies one entity into another workspace. For issue-shaped
// copies (issue/channel/dm) it follows up with a relink pass so parent and
// project links heal regardless of the order items were copied in. The relink
// is best-effort: a failure there does not fail the copy itself.
export function useCopyToWorkspace() {
  return useMutation<CopyResult, Error, { wsId: string; input: CopyToWorkspaceInput }>({
    mutationFn: async ({ wsId, input }) => {
      const result = await copyEntity(wsId, input);
      if (ISSUE_SHAPED.has(input.entityType)) {
        try {
          await relinkIssues(wsId, input.targetWorkspaceId);
        } catch {
          // The copy already landed; the relink post-pass is idempotent and
          // can be re-run later. Don't surface this as a copy failure.
        }
      }
      return result;
    },
  });
}

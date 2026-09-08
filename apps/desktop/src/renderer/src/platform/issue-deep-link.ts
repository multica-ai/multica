import { paths } from "@multica/core/paths";
import type { IssueDeepLink } from "../../../shared/issue-deep-link";
import { useTabStore } from "../stores/tab-store";

type IssueDeepLinkTabs = Pick<
  ReturnType<typeof useTabStore.getState>,
  "switchWorkspace" | "navigateActiveSession"
>;

/** Resolve a stable workspace UUID and open the Issue in the main tab model. */
export function openIssueDeepLink(
  destination: IssueDeepLink,
  workspaces: readonly { id: string; slug: string }[],
  tabs: IssueDeepLinkTabs = useTabStore.getState(),
): boolean {
  const workspace = workspaces.find(
    (candidate) => candidate.id.toLowerCase() === destination.workspaceId,
  );
  if (!workspace) return false;

  const issuePath = paths
    .workspace(workspace.slug)
    .issueDetail(destination.issueId);
  const path = destination.commentId
    ? `${issuePath}#comment-${destination.commentId}`
    : issuePath;

  // switchWorkspace opens or focuses the Issue's resource tab. Its resource
  // key deliberately ignores the hash, so replace the active session next to
  // apply a newly requested source-comment anchor to an existing tab too.
  tabs.switchWorkspace(workspace.slug, path);
  tabs.navigateActiveSession(path, { replace: true });
  return true;
}

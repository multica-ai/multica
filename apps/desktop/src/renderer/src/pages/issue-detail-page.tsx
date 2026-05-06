import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetail } from "@multica/views/issues/components";
import type { TaskChangeActions } from "@multica/views/issues/components/task-change-actions";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

// Pass-through bridge from `window.taskChangeAPI` (preload) to the
// platform-neutral `TaskChangeActions` shape consumed by IssueDetail. Web
// doesn't construct this object, so the apply UI only ever appears on
// desktop. Field names + JSON shapes mirror the preload bridge exactly —
// if either drifts, type errors surface here first.
const desktopTaskChangeActions: TaskChangeActions = {
  pickCheckoutDirectory: () => window.taskChangeAPI.pickCheckoutDirectory(),
  previewApplyTaskDiff: (input) =>
    window.taskChangeAPI.previewApplyTaskDiff(input),
  applyTaskDiff: (input) => window.taskChangeAPI.applyTaskDiff(input),
  openPath: (target) => window.taskChangeAPI.openPath(target),
};

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, id!));

  useDocumentTitle(issue ? `${issue.identifier}: ${issue.title}` : "Issue");

  if (!id) return null;
  return <IssueDetail issueId={id} taskChangeActions={desktopTaskChangeActions} />;
}

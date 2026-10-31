import type { IssueView } from "../api/schemas";
import type { WorkspacePaths } from "../paths";
import { issueViewContainerKey } from "./active-view-store";
import type { IssueViewScope } from "./queries";

export interface IssueViewTarget {
  scope: IssueViewScope;
  /** The surface the view opens on. */
  path: string;
  /** `path?view=<id>`: pages adopt it on load, so a new tab opens the view. */
  href: string;
  /** Active-view store key of that surface. */
  containerKey: string;
}

/**
 * Where a saved view opens. One resolved scope drives the path AND the
 * container key, so an unrecognised scope_type from a newer backend degrades
 * to the workspace surface coherently.
 */
export function issueViewTarget(
  wsId: string,
  view: Pick<IssueView, "id" | "scope_type" | "scope_id">,
  paths: WorkspacePaths,
): IssueViewTarget {
  const scope: IssueViewScope =
    view.scope_type === "my"
      ? { scope_type: "my" }
      : view.scope_type === "project" && view.scope_id
        ? { scope_type: "project", scope_id: view.scope_id }
        : { scope_type: "workspace" };
  const path =
    scope.scope_type === "my"
      ? paths.myIssues()
      : scope.scope_type === "project"
        ? paths.projectDetail(scope.scope_id!)
        : paths.issues();
  return {
    scope,
    path,
    href: `${path}?view=${view.id}`,
    containerKey: issueViewContainerKey(wsId, scope),
  };
}

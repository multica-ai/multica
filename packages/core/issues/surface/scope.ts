import type { IssueAssigneeType } from "../../types";

export type WorkspaceIssueActorKind = "all" | "members" | "agents";

export type IssueScope =
  | { type: "workspace"; actorKind?: WorkspaceIssueActorKind }
  | {
      type: "my";
      relation: "all" | "assigned" | "created" | "involved";
      userId: string;
    }
  | { type: "project"; projectId: string; actorKind?: WorkspaceIssueActorKind }
  | {
      type: "actor";
      actorType: Extract<IssueAssigneeType, "member" | "agent">;
      actorId: string;
      relation: "assigned" | "created";
    }
  | { type: "team"; teamId: string };

/**
 * THE single translation between the UI's coarse assignee-type tab and the
 * API's `assignee_types` values. Every channel (list GET params, table
 * query spec, gantt) must compile the tab through this function — do not
 * inline the literal arrays anywhere else.
 */
export function assigneeTypesForActorKind(
  actorKind: WorkspaceIssueActorKind | undefined,
): IssueAssigneeType[] | undefined {
  switch (actorKind) {
    case "members":
      return ["member"];
    case "agents":
      return ["agent", "squad"];
    default:
      return undefined;
  }
}

/** Saved-view scope_variant → tab. Unknown/absent variants mean "all". */
export function actorKindForViewVariant(
  variant: string | null | undefined,
): WorkspaceIssueActorKind {
  return variant === "members" || variant === "agents" ? variant : "all";
}

/** My-view scope_variant → my-scope relation. Unknown/absent means "all". */
export function myRelationForViewVariant(
  variant: string | null | undefined,
): Extract<IssueScope, { type: "my" }>["relation"] {
  return variant === "assigned" || variant === "created" || variant === "involved"
    ? variant
    : "all";
}

export class UnsupportedIssueScopeError extends Error {
  constructor(scope: IssueScope, operation: string) {
    super(`Issue scope "${issueScopeKey(scope)}" is not supported for ${operation}.`);
    this.name = "UnsupportedIssueScopeError";
  }
}

export function issueScopeKey(scope: IssueScope): string {
  switch (scope.type) {
    case "workspace":
      return `workspace:${scope.actorKind ?? "all"}`;
    case "my":
      return `my:${scope.userId}:${scope.relation}`;
    case "project":
      // The unrestricted tab keeps the historical key so existing persisted
      // display state survives; Members/Agents get their own key (and thus
      // their own per-tab display state), matching the workspace tabs.
      return scope.actorKind === "members" || scope.actorKind === "agents"
        ? `project:${scope.projectId}:${scope.actorKind}`
        : `project:${scope.projectId}`;
    case "actor":
      return `actor:${scope.actorType}:${scope.actorId}:${scope.relation}`;
    case "team":
      return `team:${scope.teamId}`;
  }
}

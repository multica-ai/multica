import type { IssueAssigneeType } from "../../types";

export type WorkspaceIssueActorKind = "all" | "members" | "agents";

export type IssueScope =
  | { type: "workspace"; actorKind?: WorkspaceIssueActorKind }
  | {
      type: "my";
      relation: "all" | "assigned" | "created" | "involved";
      userId: string;
    }
  | { type: "project"; projectId: string }
  | {
      type: "actor";
      actorType: Extract<IssueAssigneeType, "member" | "agent">;
      actorId: string;
      relation: "assigned" | "created";
    }
  | { type: "team"; teamId: string };

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
      return `project:${scope.projectId}`;
    case "actor":
      return `actor:${scope.actorType}:${scope.actorId}:${scope.relation}`;
    case "team":
      return `team:${scope.teamId}`;
  }
}

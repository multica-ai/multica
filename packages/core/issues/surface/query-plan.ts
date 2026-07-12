import type { CreateIssueRequest } from "../../types";
import type {
  AssigneeGroupedIssuesFilter,
  MyIssuesFilter,
} from "../queries";
import {
  issueScopeKey,
  UnsupportedIssueScopeError,
  type IssueScope,
} from "./scope";

export type IssueSurfaceQueryPlan =
  | {
      kind: "workspace";
      scopeKey: string;
      queryScope: undefined;
      queryFilter: MyIssuesFilter;
      groupedScopeFilter: AssigneeGroupedIssuesFilter;
      loadMoreScope: undefined;
      loadMoreFilter: undefined;
      userId: undefined;
      createDefaults: Partial<CreateIssueRequest>;
    }
  | {
      kind: "scoped";
      scopeKey: string;
      queryScope: string;
      queryFilter: MyIssuesFilter;
      groupedScopeFilter: AssigneeGroupedIssuesFilter;
      loadMoreScope: string;
      loadMoreFilter: MyIssuesFilter;
      userId?: string;
      createDefaults: Partial<CreateIssueRequest>;
    };

function myRelationPlan(
  scope: Extract<IssueScope, { type: "my" }>,
): Pick<
  Extract<IssueSurfaceQueryPlan, { kind: "scoped" }>,
  "queryScope" | "queryFilter" | "userId" | "createDefaults"
> {
  switch (scope.relation) {
    case "assigned":
      return {
        queryScope: "assigned",
        queryFilter: { assignee_id: scope.userId },
        userId: undefined,
        createDefaults: {
          assignee_type: "member",
          assignee_id: scope.userId,
        },
      };
    case "created":
      return {
        queryScope: "created",
        queryFilter: { creator_id: scope.userId },
        userId: undefined,
        createDefaults: {},
      };
    case "involved":
      return {
        queryScope: "agents",
        queryFilter: { involves_user_id: scope.userId },
        userId: undefined,
        createDefaults: {},
      };
    case "all":
      return {
        queryScope: "all",
        queryFilter: {},
        userId: scope.userId,
        createDefaults: {},
      };
  }
}

export function buildIssueSurfaceQueryPlan(
  scope: IssueScope,
): IssueSurfaceQueryPlan {
  const scopeKey = issueScopeKey(scope);

  switch (scope.type) {
    case "workspace": {
      // Members/Agents tabs are server-filtered scoped lists — assignee_types
      // is a real API param on both the list and grouped endpoints, so each
      // tab gets its own cache entry with correct per-status totals and
      // load-more pagination. Only the unfiltered "all" tab uses the shared
      // workspace list cache.
      if (scope.actorKind === "members" || scope.actorKind === "agents") {
        const queryFilter: MyIssuesFilter =
          scope.actorKind === "members"
            ? { assignee_types: ["member"] }
            : { assignee_types: ["agent", "squad"] };
        return {
          kind: "scoped",
          scopeKey,
          queryScope: scopeKey,
          queryFilter,
          groupedScopeFilter: queryFilter,
          loadMoreScope: scopeKey,
          loadMoreFilter: queryFilter,
          userId: undefined,
          createDefaults: {},
        };
      }
      return {
        kind: "workspace",
        scopeKey,
        queryScope: undefined,
        queryFilter: {},
        groupedScopeFilter: {},
        loadMoreScope: undefined,
        loadMoreFilter: undefined,
        userId: undefined,
        createDefaults: {},
      };
    }
    case "project": {
      const queryFilter = { project_id: scope.projectId };
      return {
        kind: "scoped",
        scopeKey,
        queryScope: scopeKey,
        queryFilter,
        groupedScopeFilter: queryFilter,
        loadMoreScope: scopeKey,
        loadMoreFilter: queryFilter,
        userId: undefined,
        createDefaults: { project_id: scope.projectId },
      };
    }
    case "my": {
      const plan = myRelationPlan(scope);
      return {
        kind: "scoped",
        scopeKey,
        ...plan,
        groupedScopeFilter: plan.queryFilter,
        loadMoreScope: plan.queryScope,
        loadMoreFilter: plan.queryFilter,
      };
    }
    case "actor": {
      const queryFilter =
        scope.relation === "assigned"
          ? { assignee_id: scope.actorId }
          : { creator_id: scope.actorId };
      return {
        kind: "scoped",
        scopeKey,
        queryScope: scopeKey,
        queryFilter,
        groupedScopeFilter: queryFilter,
        loadMoreScope: scopeKey,
        loadMoreFilter: queryFilter,
        userId: undefined,
        createDefaults:
          scope.relation === "assigned"
            ? {
                assignee_type: scope.actorType,
                assignee_id: scope.actorId,
              }
            : {},
      };
    }
    case "team":
      throw new UnsupportedIssueScopeError(scope, "issue surface query plan");
  }
}

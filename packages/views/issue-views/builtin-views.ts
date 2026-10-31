"use client";

import { useCallback } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import type { BuiltinViewKey } from "@multica/core/issue-views/sidebar-views";

const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;
const MY_SCOPE = { scope_type: "my" } as const;

/**
 * Where a built-in view lives, and the store write that makes its page show
 * it rather than whichever saved view was left open.
 */
export function useBuiltinViewNavigation() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const setActive = useActiveIssueViewStore((s) => s.setActive);

  const hrefFor = useCallback(
    (key: BuiltinViewKey) => {
      switch (key) {
        case "all_issues":
          return p.issues();
        case "my_agents":
          return p.myAgents();
        case "awaiting_merge":
          return p.awaitingMerge();
        case "my_issues":
          return p.myIssues();
      }
    },
    [p],
  );

  const prepare = useCallback(
    (key: BuiltinViewKey) => {
      // Its page has no saved views to leave open.
      if (key === "awaiting_merge") return;
      if (key === "all_issues") {
        setActive(issueViewContainerKey(wsId, WORKSPACE_SCOPE), null);
        return;
      }
      setActive(issueViewContainerKey(wsId, MY_SCOPE), null);
    },
    [setActive, wsId],
  );

  return { hrefFor, prepare };
}

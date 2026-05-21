"use client";

import type { Issue, IssueStatus } from "@multica/core/types";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import { ListView } from "./list-view";
import type { ChildProgress } from "./list-row";

export function TreeView(props: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  projectId?: string;
}) {
  return <ListView {...props} tree />;
}

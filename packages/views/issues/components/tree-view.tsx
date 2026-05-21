"use client";

import type { Issue, IssueStatus, Project } from "@multica/core/types";
import type { IssueSortParam, MyIssuesFilter } from "@multica/core/issues/queries";
import { ListView } from "./list-view";
import type { ChildProgress } from "./list-row";
import type { DragMoveUpdates } from "../utils/drag-utils";
import type { IssueCreateDefaults } from "../surface/types";

export function TreeView(props: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  projectId?: string;
  onMoveIssue?: (issueId: string, updates: DragMoveUpdates, onSettled?: () => void) => void;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sort?: IssueSortParam;
}) {
  return <ListView {...props} tree />;
}

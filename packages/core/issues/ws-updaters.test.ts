import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { onIssueLabelsChanged } from "./ws-updaters";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import type {
  Issue,
  IssueLabelsResponse,
  Label,
  ListIssuesCache,
} from "../types";

const WS_ID = "ws-1";
const ISSUE_ID = "issue-1";

const labelA: Label = {
  id: "label-a",
  workspace_id: WS_ID,
  name: "bug",
  color: "#ef4444",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

const labelB: Label = {
  id: "label-b",
  workspace_id: WS_ID,
  name: "feature",
  color: "#22c55e",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

const baseIssue: Issue = {
  id: ISSUE_ID,
  workspace_id: WS_ID,
  number: 1,
  identifier: "MUL-1",
  title: "Test",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  due_date: null,
  labels: [labelA],
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

describe("onIssueLabelsChanged", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("patches the per-issue label cache when present (LabelPicker source)", () => {
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID), {
      labels: [labelA],
    });

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    expect(
      qc.getQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID)),
    ).toEqual({ labels: [labelB] });
  });

  it("leaves the per-issue label cache untouched when the picker has not fetched", () => {
    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, ISSUE_ID))).toBeUndefined();
  });

  it("still patches the list and detail caches", () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: { todo: { issues: [baseIssue], total: 1 } },
    });
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.todo?.issues[0]?.labels).toEqual([labelB]);

    const detail = qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID));
    expect(detail?.labels).toEqual([labelB]);
  });
});

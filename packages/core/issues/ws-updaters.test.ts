import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from "../agents/queries";
import { onIssueDeleted, onIssueLabelsChanged } from "./ws-updaters";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import type {
  AgentActivityBucket,
  AgentRunCount,
  AgentTask,
  Attachment,
  Issue,
  IssueReaction,
  IssueLabelsResponse,
  IssueSubscriber,
  IssueUsageSummary,
  Label,
  ListIssuesCache,
  TimelineEntry,
} from "../types";

const WS_ID = "ws-1";
const ISSUE_ID = "issue-1";
const OTHER_ISSUE_ID = "issue-2";
const PARENT_ISSUE_ID = "parent-1";
const AGENT_ID = "agent-1";

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

const parentedIssue: Issue = {
  ...baseIssue,
  parent_issue_id: PARENT_ISSUE_ID,
};

const otherIssue: Issue = {
  ...baseIssue,
  id: OTHER_ISSUE_ID,
  identifier: "MUL-2",
  title: "Other",
};

function makeListCache(...issues: Issue[]): ListIssuesCache {
  return {
    byStatus: {
      todo: { issues, total: issues.length },
    },
  };
}

function makeTask(issueId = ISSUE_ID): AgentTask {
  return {
    id: `task-${issueId}`,
    agent_id: AGENT_ID,
    runtime_id: "runtime-1",
    issue_id: issueId,
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2025-01-01T00:00:00Z",
    completed_at: "2025-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2025-01-01T00:00:00Z",
  };
}

function expectInvalidated(qc: QueryClient, queryKey: readonly unknown[]) {
  expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
}

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

describe("onIssueDeleted", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("removes every cache entry scoped directly to the deleted issue", () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID), [
      {
        type: "activity",
        id: "activity-1",
        actor_type: "member",
        actor_id: "user-1",
        action: "created",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<IssueReaction[]>(issueKeys.reactions(ISSUE_ID), [
      {
        id: "reaction-1",
        issue_id: ISSUE_ID,
        actor_type: "member",
        actor_id: "user-1",
        emoji: "+1",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<IssueSubscriber[]>(issueKeys.subscribers(ISSUE_ID), [
      {
        issue_id: ISSUE_ID,
        user_type: "member",
        user_id: "user-1",
        reason: "manual",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), {
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_read_tokens: 0,
      total_cache_write_tokens: 0,
      task_count: 1,
    });
    qc.setQueryData<Attachment[]>(issueKeys.attachments(ISSUE_ID), [
      {
        id: "attachment-1",
        workspace_id: WS_ID,
        issue_id: ISSUE_ID,
        comment_id: null,
        chat_session_id: null,
        chat_message_id: null,
        uploader_type: "member",
        uploader_id: "user-1",
        filename: "evidence.png",
        url: "s3://bucket/evidence.png",
        download_url: "https://example.test/evidence.png",
        content_type: "image/png",
        size_bytes: 1,
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(ISSUE_ID), [makeTask()]);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [otherIssue]);
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue(WS_ID, ISSUE_ID), {
      labels: [labelA],
    });

    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, OTHER_ISSUE_ID), otherIssue);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(OTHER_ISSUE_ID), []);
    qc.setQueryData<IssueLabelsResponse>(
      labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID),
      { labels: [labelB] },
    );

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.timeline(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.reactions(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.subscribers(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.attachments(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.tasks(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, ISSUE_ID))).toBeUndefined();

    expect(qc.getQueryData(issueKeys.detail(WS_ID, OTHER_ISSUE_ID))).toEqual(
      otherIssue,
    );
    expect(qc.getQueryData(issueKeys.timeline(OTHER_ISSUE_ID))).toEqual([]);
    expect(qc.getQueryData(labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID))).toEqual({
      labels: [labelB],
    });
  });

  it("removes the deleted issue from workspace and my-issues list caches immediately", () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListIssuesCache>(
      issueKeys.list(WS_ID),
      makeListCache(baseIssue, otherIssue),
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", myFilter),
      makeListCache(baseIssue, otherIssue),
    );

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    const myList = qc.getQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", myFilter),
    );
    expect(list?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      OTHER_ISSUE_ID,
    ]);
    expect(list?.byStatus.todo?.total).toBe(1);
    expect(myList?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      OTHER_ISSUE_ID,
    ]);
    expect(myList?.byStatus.todo?.total).toBe(1);
    expectInvalidated(qc, issueKeys.list(WS_ID));
    expectInvalidated(qc, issueKeys.myList(WS_ID, "assigned", myFilter));
  });

  it("invalidates parent progress when the parent id only exists in detail cache", () => {
    qc.setQueryData<Issue>(
      issueKeys.detail(WS_ID, ISSUE_ID),
      parentedIssue,
    );
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [
      parentedIssue,
      otherIssue,
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const parentChildren = qc.getQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
    );
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("invalidates parent progress when the deleted issue is only present in a children cache", () => {
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [
      parentedIssue,
      otherIssue,
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const parentChildren = qc.getQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
    );
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("invalidates parent progress when the parent id only exists in a my-issues cache", () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", myFilter),
      makeListCache(parentedIssue, otherIssue),
    );
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID), [
      otherIssue,
    ]);
    qc.setQueryData(issueKeys.childProgress(WS_ID), new Map());

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    const myList = qc.getQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", myFilter),
    );
    expect(myList?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      OTHER_ISSUE_ID,
    ]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("invalidates child progress when the deleted issue is itself a parent", () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [
      {
        ...otherIssue,
        parent_issue_id: ISSUE_ID,
      },
    ]);
    qc.setQueryData(
      issueKeys.childProgress(WS_ID),
      new Map([[ISSUE_ID, { done: 0, total: 1 }]]),
    );

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("invalidates agent task and activity caches that can reference the deleted issue", () => {
    qc.setQueryData<AgentTask[]>(
      agentTaskSnapshotKeys.list(WS_ID),
      [makeTask()],
    );
    qc.setQueryData<AgentActivityBucket[]>(
      agentActivityKeys.last30d(WS_ID),
      [
        {
          agent_id: AGENT_ID,
          bucket_at: "2025-01-01T00:00:00Z",
          task_count: 1,
          failed_count: 0,
        },
      ],
    );
    qc.setQueryData<AgentRunCount[]>(agentRunCountsKeys.last30d(WS_ID), [
      { agent_id: AGENT_ID, run_count: 1 },
    ]);
    qc.setQueryData<AgentTask[]>(agentTasksKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(ISSUE_ID), [makeTask()]);

    onIssueDeleted(qc, WS_ID, ISSUE_ID);

    expectInvalidated(qc, agentTaskSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentActivityKeys.last30d(WS_ID));
    expectInvalidated(qc, agentRunCountsKeys.last30d(WS_ID));
    expectInvalidated(qc, agentTasksKeys.detail(WS_ID, AGENT_ID));
    expect(qc.getQueryData(issueKeys.tasks(ISSUE_ID))).toBeUndefined();
  });
});

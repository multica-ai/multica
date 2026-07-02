import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from "../agents/queries";
import {
  onIssueCreated,
  onIssueDeleted,
  onIssueLabelsChanged,
  onIssueMetadataChanged,
  onIssueUpdated,
} from "./ws-updaters";
import { issueKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import { projectKeys } from "../projects/queries";
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
const PROJECT_ID = "project-1";

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
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
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

  it("patches the Project Gantt cache so label filters react in place", () => {
    const PROJECT_ID = "project-1";
    qc.setQueryData<Issue[]>(issueKeys.projectGantt(WS_ID, PROJECT_ID), [
      baseIssue,
      otherIssue,
    ]);

    onIssueLabelsChanged(qc, WS_ID, ISSUE_ID, [labelB]);

    const gantt = qc.getQueryData<Issue[]>(
      issueKeys.projectGantt(WS_ID, PROJECT_ID),
    );
    expect(gantt?.find((i) => i.id === ISSUE_ID)?.labels).toEqual([labelB]);
    // Other issues in the same cache must not have their labels mutated.
    expect(gantt?.find((i) => i.id === OTHER_ISSUE_ID)?.labels).toEqual([
      labelA,
    ]);
  });
});

describe("onIssueMetadataChanged", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("replaces metadata in both detail and list caches (no merge)", () => {
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), {
      ...baseIssue,
      metadata: { pr_number: 1, stale: "yes" },
    });
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        todo: {
          issues: [{ ...baseIssue, metadata: { pr_number: 1 } }],
          total: 1,
        },
      },
    });

    onIssueMetadataChanged(qc, WS_ID, ISSUE_ID, { pr_number: 2 });

    const detail = qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID));
    expect(detail?.metadata).toEqual({ pr_number: 2 });
    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.todo?.issues[0]?.metadata).toEqual({ pr_number: 2 });
  });

  it("leaves untouched caches as undefined (no spurious writes)", () => {
    onIssueMetadataChanged(qc, WS_ID, ISSUE_ID, { foo: "bar" });

    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.list(WS_ID))).toBeUndefined();
  });
});

describe("project progress invalidation", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData(projectKeys.list(WS_ID), [
      {
        id: PROJECT_ID,
        workspace_id: WS_ID,
        title: "Project",
        description: null,
        icon: null,
        status: "in_progress",
        priority: "none",
        lead_type: null,
        lead_id: null,
        issue_count: 1,
        done_count: 0,
        resource_count: 0,
        created_at: "2025-01-01T00:00:00Z",
        updated_at: "2025-01-01T00:00:00Z",
      },
    ]);
  });

  it("invalidates project queries when an issue status changes", () => {
    onIssueUpdated(qc, WS_ID, {
      id: ISSUE_ID,
      status: "done",
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });

  it("invalidates project queries when a project issue is created", () => {
    onIssueCreated(qc, WS_ID, {
      ...baseIssue,
      project_id: PROJECT_ID,
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });
});

describe("onIssueUpdated — position move is surgical, not a list refetch", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  const issueA: Issue = { ...baseIssue, id: "issue-1", position: 0 };
  const issueB: Issue = { ...baseIssue, id: "issue-2", position: 10 };

  it("reorders the moved card in place and does NOT invalidate the workspace list", () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache(issueA, issueB));

    // issue-1 moves below issue-2 (position 0 -> 20) — a remote/echoed drag.
    onIssueUpdated(qc, WS_ID, { ...issueA, position: 20 });

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    // Surgically reordered into its new slot: proof the patch alone suffices.
    expect(list?.byStatus.todo?.issues.map((i) => i.id)).toEqual(["issue-2", "issue-1"]);
    // The old redundant `position -> invalidate(list)` is gone — no full-board
    // refetch on top of the surgical patch (that was the flicker source).
    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it("surgically patches the filtered myAll lists on a non-membership change (no refetch)", () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), makeListCache(issueA, issueB));
    qc.setQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID), makeListCache(issueA, issueB));

    // Pure position move: membership cannot change, so myAll is patched in place.
    onIssueUpdated(qc, WS_ID, { ...issueA, position: 20 });

    const my = qc.getQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID));
    expect(my?.byStatus.todo?.issues.map((i) => i.id)).toEqual(["issue-2", "issue-1"]);
    // Reconciled in place — no full-list refetch on My Issues (that was the
    // remaining drag flicker on filtered boards).
    expect(qc.getQueryState(issueKeys.myAll(WS_ID))?.isInvalidated).toBe(false);
  });

  it("removes the card from an assignee-filtered list when the assignee changes (membership-aware, no blanket refetch)", () => {
    const assignedKey = issueKeys.myListSorted(
      WS_ID,
      "assigned",
      { assignee_id: "user-1" },
      undefined,
    );
    const mine: Issue = { ...issueA, assignee_type: "member", assignee_id: "user-1" };
    qc.setQueryData<ListIssuesCache>(assignedKey, makeListCache(mine));

    onIssueUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: "member", assignee_id: "user-2" },
      { assigneeChanged: true },
    );

    // The card LEAVES the loaded list surgically — the fix for the residue
    // that used to wait on a refetch (and on the mutation path never came).
    const list = qc.getQueryData<ListIssuesCache>(assignedKey);
    expect(list?.byStatus.todo?.issues).toEqual([]);
    expect(list?.byStatus.todo?.total).toBe(0);
    expect(qc.getQueryState(assignedKey)?.isInvalidated).toBe(false);
  });

  it("flags union-scope (my:all) lists stale on an assignee change instead of guessing membership", () => {
    const myAllListKey = issueKeys.myListSorted(WS_ID, "all", {}, undefined);
    const mine: Issue = { ...issueA, assignee_type: "member", assignee_id: "user-1" };
    qc.setQueryData<ListIssuesCache>(myAllListKey, makeListCache(mine));

    onIssueUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: "member", assignee_id: "user-2" },
      { assigneeChanged: true },
    );

    // Union membership (assigned ∪ created ∪ involved) is server knowledge:
    // the card is patched in place and the list refetches to reconcile.
    const list = qc.getQueryData<ListIssuesCache>(myAllListKey);
    expect(list?.byStatus.todo?.issues[0]?.assignee_id).toBe("user-2");
    expectInvalidated(qc, myAllListKey);
  });

  it("moves the card out of the old project's list and flags the loaded target list (legacy diff fallback, no server flag)", () => {
    // issueA.project_id is null; moving it into project-9 must reconcile both
    // ends. No server flag here — this exercises the legacy cache-diff
    // fallback that keeps a new frontend working against an older backend.
    const targetKey = issueKeys.myListSorted(
      WS_ID,
      "project:project-9",
      { project_id: "project-9" },
      undefined,
    );
    qc.setQueryData<ListIssuesCache>(targetKey, makeListCache());

    onIssueUpdated(qc, WS_ID, { ...issueA, project_id: "project-9" });

    // Never hard-inserted (its page/slot is server knowledge) — the loaded
    // target list is refetched instead.
    expect(
      qc.getQueryData<ListIssuesCache>(targetKey)?.byStatus.todo?.issues,
    ).toEqual([]);
    expectInvalidated(qc, targetKey);
  });

  it("drops the card from the old project's list on a server project_changed flag even when the cached project_id already matches", () => {
    // Reproduces the drift state behind MUL-3669: the detail cache already
    // carries the NEW project (e.g. a local optimistic write), so a cache
    // diff would compute projectChanged=false — the authoritative server
    // flag must still drive the membership reconcile for any list where the
    // card lingers.
    const moved: Issue = { ...issueA, project_id: "project-9" };
    const oldProjectKey = issueKeys.myListSorted(
      WS_ID,
      "project:project-1",
      { project_id: "project-1" },
      undefined,
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, moved.id), moved);
    qc.setQueryData<ListIssuesCache>(oldProjectKey, makeListCache(moved));

    onIssueUpdated(qc, WS_ID, moved, { projectChanged: true });

    expect(
      qc.getQueryData<ListIssuesCache>(oldProjectKey)?.byStatus.todo?.issues,
    ).toEqual([]);
  });

  it("does NOT touch project lists when the server flag says project_changed=false (flag overrides the legacy diff)", () => {
    // No detail/list cache for the issue, so the legacy diff would resolve
    // oldProjectId=null and fire on the non-null incoming project_id. An explicit
    // false flag from the server is authoritative and must suppress that.
    const projectKey = issueKeys.myListSorted(
      WS_ID,
      "project:project-9",
      { project_id: "project-9" },
      undefined,
    );
    qc.setQueryData<ListIssuesCache>(projectKey, makeListCache());

    onIssueUpdated(
      qc,
      WS_ID,
      { ...issueA, project_id: "project-9" },
      { projectChanged: false },
    );

    expect(qc.getQueryState(projectKey)?.isInvalidated).toBe(false);
  });
});

// A board column header shows `byStatus[status].total`. On a status change the
// surgical patch shifts both bucket totals — but only if it can find the card in
// a loaded page. A paginated column loads just its first page, so an off-screen
// issue (very common when an agent flips the status of something the viewer
// never scrolled to) is absent: patchIssueInBuckets no-ops and the count would
// silently drift, with no refetch to recover it. The status-changed no-op has to
// fall back to a single-list refetch.
describe("onIssueUpdated — off-screen status change reconciles column counts", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("refetches the workspace list when a status-changed issue is not in the loaded page", () => {
    // First page only: the totals say these columns have items, but the issues
    // arrays are the loaded window — the moved issue lives beyond it.
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        in_review: { issues: [], total: 1 },
        done: { issues: [], total: 60 },
      },
    });

    onIssueUpdated(
      qc,
      WS_ID,
      { id: "off-screen", status: "done" },
      { statusChanged: true },
    );

    expectInvalidated(qc, issueKeys.list(WS_ID));
  });

  it("refetches the filtered myAll list under the same condition", () => {
    qc.setQueryData<ListIssuesCache>(issueKeys.myAll(WS_ID), {
      byStatus: { done: { issues: [], total: 60 } },
    });

    onIssueUpdated(
      qc,
      WS_ID,
      { id: "off-screen", status: "done" },
      { statusChanged: true },
    );

    expectInvalidated(qc, issueKeys.myAll(WS_ID));
  });

  it("does NOT refetch when the status-changed issue is loaded (surgical patch suffices)", () => {
    const loaded: Issue = { ...baseIssue, id: "loaded", status: "in_review" };
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: {
        in_review: { issues: [loaded], total: 1 },
        done: { issues: [], total: 60 },
      },
    });

    onIssueUpdated(
      qc,
      WS_ID,
      { ...loaded, status: "done" },
      { statusChanged: true },
    );

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(WS_ID));
    expect(list?.byStatus.in_review?.total).toBe(0);
    expect(list?.byStatus.done?.total).toBe(61);
    // Reconciled in place — the no-flicker fast path from #4415 must hold.
    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it("does NOT refetch an absent issue when the status did not change", () => {
    // A title/label edit of an off-screen issue cannot affect any count, so it
    // must not trigger a fallback refetch.
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), {
      byStatus: { done: { issues: [], total: 60 } },
    });

    onIssueUpdated(qc, WS_ID, { id: "off-screen", title: "renamed" });

    expect(qc.getQueryState(issueKeys.list(WS_ID))?.isInvalidated).toBe(false);
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
        markdown_url: "https://example.test/api/attachments/att-1/download",
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

// Regression coverage for the Project Gantt cache. The Gantt view rides its
// own dedicated cache (server-filtered to `scheduled=true`); every WS-driven
// path that can shift Gantt membership has to invalidate the prefix or the
// timeline goes stale.
describe("project gantt cache invalidation", () => {
  const PROJECT_ID = "project-1";
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData<Issue[]>(
      issueKeys.projectGantt(WS_ID, PROJECT_ID),
      [baseIssue],
    );
  });

  it("invalidates the project Gantt cache on issue:created", () => {
    onIssueCreated(qc, WS_ID, otherIssue);
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it("invalidates the project Gantt cache on issue:updated", () => {
    onIssueUpdated(qc, WS_ID, {
      id: ISSUE_ID,
      start_date: "2026-01-01T00:00:00Z",
    });
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it("invalidates the project Gantt cache on issue:deleted", () => {
    onIssueDeleted(qc, WS_ID, ISSUE_ID);
    expectInvalidated(qc, issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });
});

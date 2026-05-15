import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  QueryObserver,
} from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import { agentTaskSnapshotKeys, agentTasksKeys } from "@multica/core/agents/queries";
import { useBatchDeleteIssues, useDeleteIssue } from "@multica/core/issues/mutations";
import { issueKeys } from "@multica/core/issues/queries";
import { labelKeys } from "@multica/core/labels/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type {
  AgentTask,
  Attachment,
  Issue,
  IssueLabelsResponse,
  IssueUsageSummary,
  Label,
  ListIssuesCache,
  TimelineEntry,
  Workspace,
} from "@multica/core/types";

const WS_ID = "ws-1";
const SLUG = "test";
const ISSUE_ID = "issue-1";
const OTHER_ISSUE_ID = "issue-2";
const PARENT_ISSUE_ID = "parent-1";
const AGENT_ID = "agent-1";

const workspace: Workspace = {
  id: WS_ID,
  name: "Test",
  slug: SLUG,
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "TST",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const baseIssue: Issue = {
  id: ISSUE_ID,
  workspace_id: WS_ID,
  number: 1,
  identifier: "TST-1",
  title: "Deleted issue",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_issue_id: PARENT_ISSUE_ID,
  project_id: null,
  position: 0,
  due_date: null,
  labels: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const otherIssue: Issue = {
  ...baseIssue,
  id: OTHER_ISSUE_ID,
  number: 2,
  identifier: "TST-2",
  title: "Other issue",
  parent_issue_id: null,
};

const usage: IssueUsageSummary = {
  total_input_tokens: 10,
  total_output_tokens: 20,
  total_cache_read_tokens: 1,
  total_cache_write_tokens: 2,
  task_count: 1,
};

const attachment: Attachment = {
  id: "attachment-1",
  workspace_id: WS_ID,
  issue_id: ISSUE_ID,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "member",
  uploader_id: "member-1",
  filename: "evidence.png",
  url: "s3://bucket/evidence.png",
  download_url: "https://example.test/evidence.png",
  content_type: "image/png",
  size_bytes: 1,
  created_at: "2026-01-01T00:00:00Z",
};

const timeline: TimelineEntry[] = [
  {
    type: "activity",
    id: "activity-1",
    actor_type: "member",
    actor_id: "member-1",
    action: "created",
    created_at: "2026-01-01T00:00:00Z",
  },
];

const label: Label = {
  id: "label-1",
  workspace_id: WS_ID,
  name: "bug",
  color: "#ef4444",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const issueLabels: IssueLabelsResponse = {
  labels: [label],
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
    started_at: "2026-01-01T00:00:00Z",
    completed_at: "2026-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function setup(
  deleteIssue = vi.fn().mockResolvedValue(undefined),
  batchDeleteIssues = vi.fn().mockResolvedValue({ deleted: 0 }),
) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  qc.setQueryData(workspaceKeys.list(), [workspace]);
  setApiInstance({
    listWorkspaces: vi.fn().mockResolvedValue([workspace]),
    deleteIssue,
    batchDeleteIssues,
  } as any);

  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug={SLUG}>{children}</WorkspaceSlugProvider>
    </QueryClientProvider>
  );

  return { qc, deleteIssue, batchDeleteIssues, wrapper };
}

function ids(cache: ListIssuesCache | undefined) {
  return cache?.byStatus.todo?.issues.map((issue) => issue.id);
}

function expectInvalidated(qc: QueryClient, queryKey: readonly unknown[]) {
  expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
}

describe("useDeleteIssue", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("cleans list, my-list, issue-scoped, and dependent agent caches after a successful single delete", async () => {
    const { qc, deleteIssue, wrapper } = setup();
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    qc.setQueryData<ListIssuesCache>(
      issueKeys.list(WS_ID),
      makeListCache(baseIssue, otherIssue),
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", assignedFilter),
      makeListCache(baseIssue, otherIssue),
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "created", createdFilter),
      makeListCache(baseIssue),
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      [baseIssue, otherIssue],
    );
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), usage);
    qc.setQueryData<AgentTask[]>(agentTaskSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentTask[]>(agentTasksKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);

    const { result } = renderHook(() => useDeleteIssue(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(ISSUE_ID);
    });

    expect(deleteIssue).toHaveBeenCalledWith(ISSUE_ID);
    expect(ids(qc.getQueryData(issueKeys.list(WS_ID)))).toEqual([
      OTHER_ISSUE_ID,
    ]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([OTHER_ISSUE_ID]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toBeUndefined();
    expect(
      qc
        .getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID))
        ?.map((issue) => issue.id),
    ).toEqual([OTHER_ISSUE_ID]);
    expectInvalidated(qc, agentTaskSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentTasksKeys.detail(WS_ID, AGENT_ID));
  });

  it("optimistically prunes parent children without invalidating them before delete settles", async () => {
    const pendingDelete = deferred<void>();
    const { qc, wrapper } = setup(vi.fn(() => pendingDelete.promise));
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      [baseIssue, otherIssue],
    );

    const { result } = renderHook(() => useDeleteIssue(), { wrapper });
    let mutation!: Promise<void>;

    await act(async () => {
      mutation = result.current.mutateAsync(ISSUE_ID);
      await Promise.resolve();
    });

    expect(
      qc
        .getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID))
        ?.map((issue) => issue.id),
    ).toEqual([OTHER_ISSUE_ID]);
    expect(
      qc.getQueryState(issueKeys.children(WS_ID, PARENT_ISSUE_ID))
        ?.isInvalidated,
    ).not.toBe(true);

    await act(async () => {
      pendingDelete.resolve();
      await mutation;
    });

    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
  });

  it("invalidates child progress when a single delete removes a parent issue", async () => {
    const { qc, wrapper } = setup();
    const parentIssue = { ...baseIssue, parent_issue_id: null };
    const childIssue = { ...otherIssue, parent_issue_id: ISSUE_ID };
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), parentIssue);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [
      childIssue,
    ]);
    qc.setQueryData(
      issueKeys.childProgress(WS_ID),
      new Map([[ISSUE_ID, { done: 0, total: 1 }]]),
    );

    const { result } = renderHook(() => useDeleteIssue(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(ISSUE_ID);
    });

    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("restores optimistic snapshots when a single delete fails", async () => {
    const error = new Error("delete failed");
    const { qc, wrapper } = setup(vi.fn().mockRejectedValue(error));
    const assignedFilter = { assignee_id: AGENT_ID };
    const list = makeListCache(baseIssue, otherIssue);
    const myList = makeListCache(baseIssue);
    const children = [baseIssue, otherIssue];
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), list);
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", assignedFilter),
      myList,
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      children,
    );
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), usage);

    const { result } = renderHook(() => useDeleteIssue(), { wrapper });

    await expect(
      act(async () => {
        await result.current.mutateAsync(ISSUE_ID);
      }),
    ).rejects.toThrow("delete failed");

    expect(qc.getQueryData(issueKeys.list(WS_ID))).toEqual(list);
    expect(
      qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter)),
    ).toEqual(myList);
    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toEqual(
      baseIssue,
    );
    expect(qc.getQueryData(issueKeys.children(WS_ID, PARENT_ISSUE_ID))).toEqual(
      children,
    );
    expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toEqual(usage);
  });
});

describe("useBatchDeleteIssues", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("cleans list, my-list, issue-scoped, parent, and dependent agent caches after a fully successful batch delete", async () => {
    const batchDeleteIssues = vi.fn().mockResolvedValue({ deleted: 2 });
    const { qc, wrapper } = setup(undefined, batchDeleteIssues);
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const idsToDelete = [ISSUE_ID, OTHER_ISSUE_ID];
    const childIssue = { ...otherIssue, parent_issue_id: PARENT_ISSUE_ID };
    qc.setQueryData<ListIssuesCache>(
      issueKeys.list(WS_ID),
      makeListCache(baseIssue, childIssue),
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", assignedFilter),
      makeListCache(baseIssue, childIssue),
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "created", createdFilter),
      makeListCache(baseIssue),
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, OTHER_ISSUE_ID), childIssue);
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      [baseIssue, childIssue],
    );
    qc.setQueryData(issueKeys.childProgress(WS_ID), {});
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), usage);
    qc.setQueryData<IssueUsageSummary>(
      issueKeys.usage(OTHER_ISSUE_ID),
      usage,
    );
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(ISSUE_ID), [makeTask()]);
    qc.setQueryData<AgentTask[]>(issueKeys.tasks(OTHER_ISSUE_ID), [
      makeTask(OTHER_ISSUE_ID),
    ]);
    qc.setQueryData<AgentTask[]>(agentTaskSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentTask[]>(agentTasksKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);

    const { result } = renderHook(() => useBatchDeleteIssues(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(idsToDelete);
    });

    expect(batchDeleteIssues).toHaveBeenCalledWith(idsToDelete);
    expect(ids(qc.getQueryData(issueKeys.list(WS_ID)))).toEqual([]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toBeUndefined();
    expect(
      qc.getQueryData(issueKeys.detail(WS_ID, OTHER_ISSUE_ID)),
    ).toBeUndefined();
    expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.usage(OTHER_ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.tasks(ISSUE_ID))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.tasks(OTHER_ISSUE_ID))).toBeUndefined();
    expect(
      qc.getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID)),
    ).toEqual([]);
    expectInvalidated(qc, issueKeys.children(WS_ID, PARENT_ISSUE_ID));
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
    expectInvalidated(qc, agentTaskSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentTasksKeys.detail(WS_ID, AGENT_ID));
  });

  it("invalidates child progress when a full batch delete removes a parent issue", async () => {
    const batchDeleteIssues = vi.fn().mockResolvedValue({ deleted: 1 });
    const { qc, wrapper } = setup(undefined, batchDeleteIssues);
    const parentIssue = { ...baseIssue, parent_issue_id: null };
    const childIssue = { ...otherIssue, parent_issue_id: ISSUE_ID };
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), parentIssue);
    qc.setQueryData<Issue[]>(issueKeys.children(WS_ID, ISSUE_ID), [
      childIssue,
    ]);
    qc.setQueryData(
      issueKeys.childProgress(WS_ID),
      new Map([[ISSUE_ID, { done: 0, total: 1 }]]),
    );

    const { result } = renderHook(() => useBatchDeleteIssues(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync([ISSUE_ID]);
    });

    expect(batchDeleteIssues).toHaveBeenCalledWith([ISSUE_ID]);
    expect(qc.getQueryData(issueKeys.children(WS_ID, ISSUE_ID))).toBeUndefined();
    expectInvalidated(qc, issueKeys.childProgress(WS_ID));
  });

  it("restores optimistic list snapshots on partial batch delete before invalidating caches", async () => {
    const batchDeleteIssues = vi.fn().mockResolvedValue({ deleted: 1 });
    const { qc, wrapper } = setup(undefined, batchDeleteIssues);
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const idsToDelete = [ISSUE_ID, OTHER_ISSUE_ID];
    const childIssue = { ...otherIssue, parent_issue_id: PARENT_ISSUE_ID };
    const list = makeListCache(baseIssue, childIssue);
    const assignedMyList = makeListCache(baseIssue, childIssue);
    const createdMyList = makeListCache(baseIssue);
    const children = [baseIssue, childIssue];
    qc.setQueryData<ListIssuesCache>(
      issueKeys.list(WS_ID),
      list,
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", assignedFilter),
      assignedMyList,
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "created", createdFilter),
      createdMyList,
    );
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      children,
    );
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID), baseIssue);
    qc.setQueryData<Issue>(issueKeys.detail(WS_ID, OTHER_ISSUE_ID), childIssue);
    qc.setQueryData<IssueUsageSummary>(issueKeys.usage(ISSUE_ID), usage);
    qc.setQueryData<Attachment[]>(issueKeys.attachments(ISSUE_ID), [
      attachment,
    ]);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID), timeline);
    qc.setQueryData<IssueLabelsResponse>(
      labelKeys.byIssue(WS_ID, ISSUE_ID),
      issueLabels,
    );
    qc.setQueryData<IssueUsageSummary>(
      issueKeys.usage(OTHER_ISSUE_ID),
      usage,
    );
    qc.setQueryData<Attachment[]>(issueKeys.attachments(OTHER_ISSUE_ID), [
      { ...attachment, id: "attachment-2", issue_id: OTHER_ISSUE_ID },
    ]);
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(OTHER_ISSUE_ID), [
      { ...timeline[0]!, id: "activity-2" },
    ]);
    qc.setQueryData<IssueLabelsResponse>(
      labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID),
      issueLabels,
    );
    qc.setQueryData<AgentTask[]>(agentTaskSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentTask[]>(agentTasksKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);
    const listRefetch = deferred<ListIssuesCache>();
    const listObserver = new QueryObserver(qc, {
      queryKey: issueKeys.list(WS_ID),
      queryFn: () => listRefetch.promise,
      refetchOnMount: false,
      staleTime: Infinity,
    });
    const unsubscribeList = listObserver.subscribe(() => {});

    const { result } = renderHook(() => useBatchDeleteIssues(), { wrapper });

    try {
      await act(async () => {
        await result.current.mutateAsync(idsToDelete);
      });

      expect(batchDeleteIssues).toHaveBeenCalledWith(idsToDelete);
      expect(qc.getQueryData(issueKeys.list(WS_ID))).toEqual(list);
      expect(
        qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter)),
      ).toEqual(assignedMyList);
      expect(
        qc.getQueryData(issueKeys.myList(WS_ID, "created", createdFilter)),
      ).toEqual(createdMyList);
      expect(
        qc.getQueryData(issueKeys.children(WS_ID, PARENT_ISSUE_ID)),
      ).toEqual(children);
      expect(qc.getQueryData(issueKeys.detail(WS_ID, ISSUE_ID))).toEqual(
        baseIssue,
      );
      expect(qc.getQueryData(issueKeys.detail(WS_ID, OTHER_ISSUE_ID))).toEqual(
        childIssue,
      );
      expect(qc.getQueryData(issueKeys.usage(ISSUE_ID))).toEqual(usage);
      expect(qc.getQueryData(issueKeys.attachments(ISSUE_ID))).toEqual([
        attachment,
      ]);
      expect(qc.getQueryData(issueKeys.timeline(ISSUE_ID))).toEqual(timeline);
      expect(qc.getQueryData(labelKeys.byIssue(WS_ID, ISSUE_ID))).toEqual(
        issueLabels,
      );
      expect(qc.getQueryData(issueKeys.usage(OTHER_ISSUE_ID))).toEqual(usage);
      expect(qc.getQueryData(issueKeys.attachments(OTHER_ISSUE_ID))).toEqual([
        { ...attachment, id: "attachment-2", issue_id: OTHER_ISSUE_ID },
      ]);
      expect(qc.getQueryData(issueKeys.timeline(OTHER_ISSUE_ID))).toEqual([
        { ...timeline[0]!, id: "activity-2" },
      ]);
      expect(qc.getQueryData(labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID))).toEqual(
        issueLabels,
      );
      expectInvalidated(qc, issueKeys.detail(WS_ID, ISSUE_ID));
      expectInvalidated(qc, issueKeys.detail(WS_ID, OTHER_ISSUE_ID));
      expectInvalidated(qc, issueKeys.usage(ISSUE_ID));
      expectInvalidated(qc, issueKeys.attachments(ISSUE_ID));
      expectInvalidated(qc, issueKeys.timeline(ISSUE_ID));
      expectInvalidated(qc, labelKeys.byIssue(WS_ID, ISSUE_ID));
      expectInvalidated(qc, issueKeys.usage(OTHER_ISSUE_ID));
      expectInvalidated(qc, issueKeys.attachments(OTHER_ISSUE_ID));
      expectInvalidated(qc, issueKeys.timeline(OTHER_ISSUE_ID));
      expectInvalidated(qc, labelKeys.byIssue(WS_ID, OTHER_ISSUE_ID));
      expectInvalidated(qc, agentTaskSnapshotKeys.list(WS_ID));
      expectInvalidated(qc, agentTasksKeys.detail(WS_ID, AGENT_ID));
    } finally {
      unsubscribeList();
      listRefetch.resolve(list);
    }
  });

  it("restores optimistic workspace, my-list, and parent children snapshots when a batch delete fails", async () => {
    const pendingDelete = deferred<{ deleted: number }>();
    const { qc, wrapper } = setup(undefined, vi.fn(() => pendingDelete.promise));
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const list = makeListCache(baseIssue, otherIssue);
    const assignedMyList = makeListCache(baseIssue, otherIssue);
    const createdMyList = makeListCache(baseIssue);
    const children = [baseIssue, otherIssue];
    qc.setQueryData<ListIssuesCache>(issueKeys.list(WS_ID), list);
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "assigned", assignedFilter),
      assignedMyList,
    );
    qc.setQueryData<ListIssuesCache>(
      issueKeys.myList(WS_ID, "created", createdFilter),
      createdMyList,
    );
    qc.setQueryData<Issue[]>(
      issueKeys.children(WS_ID, PARENT_ISSUE_ID),
      children,
    );

    const { result } = renderHook(() => useBatchDeleteIssues(), { wrapper });
    let mutation!: Promise<{ deleted: number }>;

    await act(async () => {
      mutation = result.current.mutateAsync([ISSUE_ID]);
      await Promise.resolve();
    });

    expect(ids(qc.getQueryData(issueKeys.list(WS_ID)))).toEqual([
      OTHER_ISSUE_ID,
    ]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([OTHER_ISSUE_ID]);
    expect(
      ids(qc.getQueryData(issueKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(
      qc
        .getQueryData<Issue[]>(issueKeys.children(WS_ID, PARENT_ISSUE_ID))
        ?.map((issue) => issue.id),
    ).toEqual([OTHER_ISSUE_ID]);

    await expect(
      act(async () => {
        pendingDelete.reject(new Error("batch delete failed"));
        await mutation;
      }),
    ).rejects.toThrow("batch delete failed");

    expect(qc.getQueryData(issueKeys.list(WS_ID))).toEqual(list);
    expect(
      qc.getQueryData(issueKeys.myList(WS_ID, "assigned", assignedFilter)),
    ).toEqual(assignedMyList);
    expect(
      qc.getQueryData(issueKeys.myList(WS_ID, "created", createdFilter)),
    ).toEqual(createdMyList);
    expect(qc.getQueryData(issueKeys.children(WS_ID, PARENT_ISSUE_ID))).toEqual(
      children,
    );
  });
});

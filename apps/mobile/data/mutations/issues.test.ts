// @vitest-environment node
import { MutationObserver, onlineManager, QueryClient } from "@tanstack/react-query";
import { EMPTY_APP_CONFIG } from "@multica/core/api/schemas";
import type { AppConfigResponse } from "@multica/core/api/schemas";
import type { AgentTask, Issue, TimelineEntry } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { appConfigOptions } from "@/data/queries/billing";
import { issueKeys } from "@/data/queries/issue-keys";
import { useCancelTask, useDeleteComment } from "./issues";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  wsId: "workspace-1",
  observer: undefined as unknown as MutationObserver<unknown, unknown, unknown, unknown>,
  reuseObserver: false,
  result: () => ({} as { isPending: boolean; isPaused: boolean; isSuccess: boolean; isError: boolean }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => state.qc,
    // Drive the hook's options through a real MutationObserver so the test
    // runs the same mutate → onSuccess → onSettled lifecycle as the app.
    useMutation: (
      options: ConstructorParameters<typeof actual.MutationObserver>[1],
    ) => {
      const observer = state.reuseObserver
        ? state.observer
        : new actual.MutationObserver(state.qc, options);
      observer.setOptions(options);
      state.observer = observer;
      state.result = () => observer.getCurrentResult();
      return { mutateAsync: (variables: unknown) => observer.mutate(variables) };
    },
  };
});

vi.mock("@/data/api", () => ({
  api: {
    deleteComment: vi.fn(async () => undefined),
    cancelTaskById: vi.fn(async () => undefined),
  },
}));

vi.mock("@/data/auth-store", () => ({ useAuthStore: vi.fn() }));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: state.wsId }),
}));

const wsId = "workspace-1";
const issueId = "issue-1";
const timelineKey = issueKeys.timeline(wsId, issueId);

function comment(id: string, parentId: string | null = null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    created_at: "2026-09-11T07:00:00Z",
    content: `content of ${id}`,
    parent_id: parentId,
  };
}

// A thread whose root has a reply chain, plus an unrelated comment.
function seedThread() {
  state.qc.setQueryData<TimelineEntry[]>(timelineKey, [
    comment("comment-1"),
    comment("reply-1", "comment-1"),
    comment("reply-2", "reply-1"),
    comment("comment-2"),
  ]);
}

describe("useDeleteComment", () => {
  beforeEach(() => {
    state.qc = new QueryClient();
    vi.mocked(api.deleteComment).mockClear();
  });

  it("uses the keep-replies route and tombstones a comment with replies when the server declares it", async () => {
    state.qc.setQueryData(appConfigOptions().queryKey, {
      ...EMPTY_APP_CONFIG,
      comment_delete_keep_replies_supported: true,
    });
    seedThread();

    await useDeleteComment(issueId).mutateAsync("comment-1");

    expect(api.deleteComment).toHaveBeenCalledWith("comment-1", { keepReplies: true });
    const timeline = state.qc.getQueryData<TimelineEntry[]>(timelineKey);
    expect(timeline?.map((e) => e.id)).toEqual(["comment-1", "reply-1", "reply-2", "comment-2"]);
    expect(timeline?.[0]).toMatchObject({ content: "" });
    expect(timeline?.[0]?.deleted_at).toEqual(expect.any(String));
  });

  it.each<[string, AppConfigResponse | undefined]>([
    ["has not loaded", undefined],
    ["omits the capability", { cdn_domain: "", allow_signup: true }],
    ["declares it false", { ...EMPTY_APP_CONFIG, comment_delete_keep_replies_supported: false }],
  ])("uses the legacy route and drops every cached reply when the config %s", async (_label, config) => {
    if (config) state.qc.setQueryData(appConfigOptions().queryKey, config);
    seedThread();

    await useDeleteComment(issueId).mutateAsync("comment-1");

    expect(api.deleteComment).toHaveBeenCalledWith("comment-1", { keepReplies: false });
    expect(state.qc.getQueryData<TimelineEntry[]>(timelineKey)?.map((e) => e.id)).toEqual([
      "comment-2",
    ]);
  });

  it("refetches the owner issue projections because the 204 carries no issue_revision", async () => {
    const issue = { id: issueId, revision: 3 } as Issue;
    const myKey = issueKeys.myList(wsId, "assigned", { assignee_id: "user-1" });
    state.qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), issue);
    state.qc.setQueryData<Issue[]>(myKey, [issue]);
    state.qc.setQueryData<Issue[]>(issueKeys.list(wsId), [issue]);

    await useDeleteComment(issueId).mutateAsync("comment-1");

    for (const key of [issueKeys.detail(wsId, issueId), myKey, issueKeys.list(wsId)]) {
      expect(state.qc.getQueryState(key)?.isInvalidated).toBe(true);
    }
  });
});

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

const activeKey = issueKeys.activeTasks(wsId, issueId);
const tasksKey = issueKeys.tasks(wsId, issueId);
const run = { id: "task-1", status: "running" } as AgentTask;
const otherRun = { id: "task-2", status: "queued" } as AgentTask;

describe("useCancelTask", () => {
  beforeEach(() => {
    onlineManager.setOnline(true);
    state.wsId = wsId;
    state.reuseObserver = false;
    state.qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    state.qc.mount();
    state.qc.setQueryData(activeKey, [run, otherRun]);
    state.qc.setQueryData(tasksKey, [run, otherRun]);
    vi.mocked(api.cancelTaskById).mockReset();
  });

  afterEach(() => {
    onlineManager.setOnline(true);
    state.qc.unmount();
    state.qc.clear();
    state.reuseObserver = false;
    state.wsId = wsId;
  });

  it("keeps the run visible while the API is pending and refreshes both lists after acknowledgement", async () => {
    const request = deferred();
    vi.mocked(api.cancelTaskById).mockReturnValue(request.promise);
    const pending = useCancelTask(issueId).mutateAsync(run.id);
    await vi.waitFor(() => expect(api.cancelTaskById).toHaveBeenCalledWith(run.id));
    const visibleBeforeAcknowledgement = state.qc.getQueryData(activeKey);
    expect(state.result().isPending).toBe(true);
    request.resolve();
    await pending;

    expect(visibleBeforeAcknowledgement).toEqual([run, otherRun]);
    // The cached list changes only when an authoritative refetch/WS update arrives.
    expect(state.qc.getQueryData(activeKey)).toEqual([run, otherRun]);
    expect(state.result().isSuccess).toBe(true);
    expect(state.qc.getQueryState(activeKey)?.isInvalidated).toBe(true);
    expect(state.qc.getQueryState(tasksKey)?.isInvalidated).toBe(true);
  });

  it("exposes rejection without rolling a newer authoritative cache update back", async () => {
    const request = deferred();
    vi.mocked(api.cancelTaskById).mockReturnValue(request.promise);
    const pending = useCancelTask(issueId).mutateAsync(run.id);
    const rejected = expect(pending).rejects.toThrow("Stop rejected");
    await vi.waitFor(() => expect(api.cancelTaskById).toHaveBeenCalledOnce());
    // Another run completes while this request is in flight.
    state.qc.setQueryData(activeKey, [run]);
    request.reject(new Error("Stop rejected"));
    await rejected;

    expect(state.result().isError).toBe(true);
    expect(state.qc.getQueryData(activeKey)).toEqual([run]);
    expect(state.qc.getQueryState(activeKey)?.isInvalidated).toBe(true);
    expect(state.qc.getQueryState(tasksKey)?.isInvalidated).toBe(true);
  });

  it("keeps offline-paused runs visible before any request and resumes on reconnect", async () => {
    onlineManager.setOnline(false);
    vi.mocked(api.cancelTaskById).mockResolvedValue(undefined);
    const pending = useCancelTask(issueId).mutateAsync(run.id);
    await vi.waitFor(() => expect(state.result().isPaused).toBe(true));
    // Let async onMutate finish even though the mutation is already paused.
    await new Promise((resolve) => setTimeout(resolve, 0));
    const visibleWhileOffline = state.qc.getQueryData(activeKey);
    expect(api.cancelTaskById).not.toHaveBeenCalled();
    onlineManager.setOnline(true);
    await pending;

    expect(visibleWhileOffline).toEqual([run, otherRun]);
    expect(api.cancelTaskById).toHaveBeenCalledExactlyOnceWith(run.id);
    expect(state.result().isSuccess).toBe(true);
  });

  it("invalidates only the originating workspace and issue after a workspace switch", async () => {
    const otherWorkspace = issueKeys.activeTasks("workspace-2", issueId);
    const otherIssue = issueKeys.activeTasks(wsId, "issue-2");
    state.qc.setQueryData(otherWorkspace, [otherRun]);
    state.qc.setQueryData(otherIssue, [otherRun]);
    const request = deferred();
    vi.mocked(api.cancelTaskById).mockReturnValue(request.promise);
    const pending = useCancelTask(issueId).mutateAsync(run.id);
    await vi.waitFor(() => expect(api.cancelTaskById).toHaveBeenCalledOnce());
    state.wsId = "workspace-2";
    // A real mounted hook updates its pending mutation options on rerender.
    state.reuseObserver = true;
    useCancelTask(issueId);
    request.resolve();
    await pending;

    expect(state.qc.getQueryState(activeKey)?.isInvalidated).toBe(true);
    for (const key of [otherWorkspace, otherIssue]) {
      expect(state.qc.getQueryData(key)).toEqual([otherRun]);
      expect(state.qc.getQueryState(key)?.isInvalidated).toBe(false);
    }
  });
});

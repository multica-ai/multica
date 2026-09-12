import { QueryClient } from "@tanstack/react-query";
import { EMPTY_APP_CONFIG } from "@multica/core/api/schemas";
import type { AppConfigResponse } from "@multica/core/api/schemas";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { appConfigOptions } from "@/data/queries/billing";
import { issueKeys } from "@/data/queries/issue-keys";
import { useDeleteComment } from "./issues";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
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
      const observer = new actual.MutationObserver(state.qc, options);
      return { mutateAsync: (variables: unknown) => observer.mutate(variables) };
    },
  };
});

vi.mock("@/data/api", () => ({
  api: { deleteComment: vi.fn(async () => undefined) },
}));

vi.mock("@/data/auth-store", () => ({ useAuthStore: vi.fn() }));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: "workspace-1" }),
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

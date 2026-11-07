// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { inboxKeys } from "@/data/queries/inbox";
import { useMarkInboxRead } from "./inbox";

const state = vi.hoisted(() => ({ qc: undefined as unknown as QueryClient }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => state.qc,
    // Keep the real mutation lifecycle, substituting only React hook wiring.
    useMutation: (
      options: ConstructorParameters<typeof actual.MutationObserver>[1],
    ) => {
      const observer = new actual.MutationObserver(state.qc, options);
      return { mutateAsync: (variables: unknown) => observer.mutate(variables) };
    },
  };
});

vi.mock("@/data/api", () => ({
  api: { markInboxRead: vi.fn() },
}));

vi.mock("@/data/realtime/inbox-ws-updaters", () => ({
  refreshInboxList: vi.fn(),
  refreshInboxUnreadSummary: vi.fn(),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: "workspace-1" }),
}));

const wsId = "workspace-1";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: wsId,
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function readState() {
  const items = state.qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId)) ?? [];
  return Object.fromEntries(items.map((i) => [i.id, i.read]));
}

describe("useMarkInboxRead", () => {
  beforeEach(() => {
    state.qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    state.qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      item({ id: "comment" }),
      item({ id: "assigned", type: "issue_assigned", severity: "action_required" }),
      item({ id: "archived-sibling", archived: true }),
      item({ id: "other-issue", issue_id: "issue-2" }),
      item({ id: "no-issue", issue_id: null }),
    ]);
  });

  // MUL-8618: the server reads the issue's other unread active rows with the
  // one the row shows; the optimistic patch covers the same rows.
  it("reads the item and every other active item on its issue", async () => {
    vi.mocked(api.markInboxRead).mockResolvedValue(item({ id: "comment", read: true }));
    const { mutateAsync } = useMarkInboxRead();

    await mutateAsync("comment");

    expect(readState()).toEqual({
      comment: true,
      assigned: true,
      "archived-sibling": false,
      "other-issue": false,
      "no-issue": false,
    });
  });

  it("restores the list as it was before the patch when the request fails", async () => {
    vi.mocked(api.markInboxRead).mockRejectedValue(new Error("boom"));
    const { mutateAsync } = useMarkInboxRead();

    await expect(mutateAsync("comment")).rejects.toThrow("boom");

    expect(readState()).toEqual({
      comment: false,
      assigned: false,
      "archived-sibling": false,
      "other-issue": false,
      "no-issue": false,
    });
  });
});

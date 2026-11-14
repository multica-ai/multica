import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@multica/core/query-client";
import { inboxKeys, useInboxUnreadCount } from "@multica/core/inbox/queries";
import { EMPTY_INBOX_FILTERS, activeInboxFilterSupport, effectiveInboxFilters } from "@multica/core/inbox/filter-store";
import type { AgentTask, InboxItem } from "@multica/core/types";
import { useInboxSections } from "./use-inbox-sections";

const mocks = vi.hoisted(() => ({ tasks: [] as unknown[], summary: vi.fn() }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) => selector({ user: { id: "me" } }),
}));
vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["test-tasks"], queryFn: async () => mocks.tasks }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_ws: string, id: string) => ({
    queryKey: ["test-issue", id],
    queryFn: async () => ({ id, project_id: "project-b", labels: [] }),
  }),
}));
vi.mock("@multica/core/api", () => ({ api: { getInboxUnreadSummary: mocks.summary } }));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  mocks.tasks = [];
});

describe("useInboxSections", () => {
  it("re-reads the badge when a snoozed issue comes back, with no other event", async () => {
    vi.useFakeTimers();
    const start = Date.parse("2026-11-14T12:00:00Z");
    vi.setSystemTime(start);
    const row = {
      id: "n",
      issue_id: "i",
      section: "needs_you",
      archived: false,
      read: false,
      created_at: new Date(start).toISOString(),
      snoozed_until: new Date(start + 1000).toISOString(),
    } as InboxItem;
    const qc = createQueryClient();
    qc.setQueryData(["test-tasks"], []);
    qc.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws", count: 0, needs_you: 0 }]);
    mocks.summary.mockResolvedValue([{ workspace_id: "ws", count: 1, needs_you: 1 }]);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(
      () => ({
        sections: useInboxSections("ws", [row], [row], { filters: EMPTY_INBOX_FILTERS }),
        badge: useInboxUnreadCount("ws"),
      }),
      { wrapper },
    );
    expect(result.current.sections.snoozed).toHaveLength(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1100);
    });

    expect(result.current.sections.needsYou).toHaveLength(1);
    expect(result.current.badge).toBe(1);
  });

  it("keeps narrowing Working when no notification is left", async () => {
    mocks.tasks = [
      {
        id: "t",
        issue_id: "i",
        agent_id: "a",
        status: "running",
        started_at: "2026-11-14T12:00:00Z",
        created_at: "2026-11-14T12:00:00Z",
        attribution: { source: "direct_human", originator: { id: "me" } },
      } as AgentTask,
    ];
    const rawItems: InboxItem[] = [];
    const filters = effectiveInboxFilters(
      { ...EMPTY_INBOX_FILTERS, projects: ["project-a"] },
      activeInboxFilterSupport(rawItems),
    );
    const qc = createQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useInboxSections("ws", rawItems, rawItems, { filters }), { wrapper });

    await waitFor(() => expect(result.current.workingPending).toBe(false));
    expect(result.current.working).toHaveLength(0);
  });
});

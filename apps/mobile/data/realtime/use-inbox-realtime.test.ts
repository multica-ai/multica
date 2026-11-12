// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { inboxKeys } from "@/data/queries/inbox";
import { useInboxRealtime } from "./use-inbox-realtime";

interface MockWS {
  on: ReturnType<typeof vi.fn>;
  onReconnect: ReturnType<typeof vi.fn>;
}
type SubscriptionSetup = (ws: MockWS, wsId: string) => (() => void)[];
const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  setup: undefined as unknown as SubscriptionSetup,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => state.qc,
}));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: SubscriptionSetup) => { state.setup = setup; },
}));
vi.mock("@/data/api", () => ({ api: {} }));

describe("mobile inbox realtime", () => {
  beforeEach(() => { state.qc = new QueryClient(); });

  it("refreshes both the list and unread summary on deletion from another client", async () => {
    const listKey = inboxKeys.list("workspace-1");
    const summaryKey = inboxKeys.unreadSummary();
    state.qc.setQueryData(listKey, [{ id: "notification-1" }]);
    state.qc.setQueryData(summaryKey, [{ workspace_id: "workspace-1", count: 1 }]);
    const handlers = new Map<string, () => void>();
    const ws: MockWS = {
      on: vi.fn((event: string, handler: () => void) => {
        handlers.set(event, handler);
        return () => handlers.delete(event);
      }),
      onReconnect: vi.fn(() => () => {}),
    };
    useInboxRealtime();
    const unsubs = state.setup(ws, "workspace-1");
    const deleted = handlers.get("inbox:deleted");
    expect(deleted).toBeDefined();
    deleted!();
    await vi.waitFor(() => {
      expect(state.qc.getQueryState(listKey)?.isInvalidated).toBe(true);
      expect(state.qc.getQueryState(summaryKey)?.isInvalidated).toBe(true);
    });
    for (const unsubscribe of unsubs) unsubscribe();
    expect(handlers.has("inbox:deleted")).toBe(false);
  });
});

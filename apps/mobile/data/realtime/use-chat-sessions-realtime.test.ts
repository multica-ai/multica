import { beforeEach, describe, expect, it, vi } from "vitest";

const { invalidateQueries, subscriptionSetups } = vi.hoisted(() => ({
  invalidateQueries: vi.fn(),
  subscriptionSetups: [] as Array<(ws: MockWS, wsId: string) => Array<() => void>>,
}));

type EventHandler = (payload: unknown) => void;

interface MockWS {
  on: ReturnType<typeof vi.fn>;
  onReconnect: ReturnType<typeof vi.fn>;
}

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries }),
}));

vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: (ws: MockWS, wsId: string) => Array<() => void>) => {
    subscriptionSetups.push(setup);
  },
}));

vi.mock("@/data/api", () => ({ api: {} }));

import { chatKeys } from "@/data/queries/chat";
import { useChatSessionsRealtime } from "./use-chat-sessions-realtime";

describe("useChatSessionsRealtime", () => {
  beforeEach(() => {
    invalidateQueries.mockReset();
    subscriptionSetups.length = 0;
  });

  it.each(["chat:done", "task:completed", "task:failed", "task:cancelled"])(
    "marks offscreen session snapshots stale on %s without background fetching", (event) => {
      useChatSessionsRealtime();
      const handlers = new Map<string, EventHandler>();
      subscriptionSetups[0]({
        on: vi.fn((name: string, handler: EventHandler) => {
          handlers.set(name, handler); return () => {};
        }),
        onReconnect: vi.fn(() => () => {}),
      }, "workspace-1");
      handlers.get(event)!({ chat_session_id: "offscreen" });
      for (const queryKey of [chatKeys.messages("offscreen"), chatKeys.pendingTask("offscreen")]) {
        expect(invalidateQueries).toHaveBeenCalledWith({ queryKey, type: "inactive", refetchType: "none" });
      }
      invalidateQueries.mockClear();
      handlers.get(event)!({ issue_id: "issue-task" });
      expect(invalidateQueries).not.toHaveBeenCalled();
    },
  );

  it("invalidates the workspace session list for channel-created chats", () => {
    useChatSessionsRealtime();
    expect(subscriptionSetups).toHaveLength(1);

    const handlers = new Map<string, EventHandler>();
    const ws: MockWS = {
      on: vi.fn((event: string, handler: EventHandler) => {
        handlers.set(event, handler);
        return () => {};
      }),
      onReconnect: vi.fn(() => () => {}),
    };

    subscriptionSetups[0](ws, "workspace-1");
    handlers.get("chat:session_created")?.({
      workspace_id: "workspace-1",
      chat_session_id: "channel-session-1",
    });

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: chatKeys.sessions("workspace-1"),
    });

    invalidateQueries.mockClear();
    handlers.get("chat:session_created")?.({
      workspace_id: "workspace-2",
      chat_session_id: "other-workspace-session",
    });
    expect(invalidateQueries).not.toHaveBeenCalled();
  });
});

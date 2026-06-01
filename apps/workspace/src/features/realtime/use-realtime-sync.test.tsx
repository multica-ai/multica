import React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WSMessage } from "@/shared/types";
import { queryKeys } from "@/shared/query";
import { useRealtimeSync } from "./use-realtime-sync";

const issueStoreState = {
  addIssue: vi.fn(),
  updateIssue: vi.fn(),
  removeIssue: vi.fn(),
};

const inboxStoreState = {
  addItem: vi.fn(),
  updateIssueStatus: vi.fn(),
};

const workspaceStoreState = {
  workspace: { id: "ws-1" },
  refreshWorkspaces: vi.fn(),
};

const authStoreState = {
  user: { id: "user-1" },
};

vi.mock("sonner", () => ({
  toast: { info: vi.fn() },
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: {
    getState: () => issueStoreState,
  },
}));

vi.mock("@/features/inbox", () => ({
  useInboxStore: {
    getState: () => inboxStoreState,
  },
}));

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: {
    getState: () => workspaceStoreState,
  },
}));

vi.mock("@/features/auth", () => ({
  useAuthStore: {
    getState: () => authStoreState,
  },
}));

class FakeWSClient {
  private anyHandlers = new Set<(msg: WSMessage) => void>();
  private eventHandlers = new Map<string, Set<(payload: unknown) => void>>();
  private reconnectHandlers = new Set<() => void>();

  on(event: string, handler: (payload: unknown) => void) {
    if (!this.eventHandlers.has(event)) {
      this.eventHandlers.set(event, new Set());
    }
    this.eventHandlers.get(event)?.add(handler);
    return () => {
      this.eventHandlers.get(event)?.delete(handler);
    };
  }

  onAny(handler: (msg: WSMessage) => void) {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  onReconnect(handler: () => void) {
    this.reconnectHandlers.add(handler);
    return () => {
      this.reconnectHandlers.delete(handler);
    };
  }

  emit(message: WSMessage) {
    this.eventHandlers.get(message.type)?.forEach((handler) => handler(message.payload));
    this.anyHandlers.forEach((handler) => handler(message));
  }
}

describe("useRealtimeSync", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("refreshes issues for same-user issue imports across tabs", async () => {
    const ws = new FakeWSClient();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    renderHook(() => useRealtimeSync(ws as never), { wrapper });

    await act(async () => {
      ws.emit({
        type: "issue:imported" as never,
        payload: { created: 2 },
        actor_id: "user-1",
      });
      await Promise.resolve();
    });

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: queryKeys.issues.all() });
  });
});

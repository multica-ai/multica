import { useLayoutEffect, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore } from "@multica/core/chat";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { ChatSession, Project, StorageAdapter } from "@multica/core/types";
import { useChatController } from "./use-chat-controller";

const route = vi.hoisted(() => ({ workspaceId: "ws-source", isActive: true }));
const data = vi.hoisted(() => ({
  sessions: {} as Record<string, ChatSession[]>,
  projects: {} as Record<string, Project[]>,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => route.workspaceId,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: () => false,
  useAgentPresenceDetail: () => ({ availability: "online" }),
  useCustomizeConversationStartersHref: () => null,
  useWorkspaceAgentAvailability: () => "available",
}));
vi.mock("../../common/use-app-foreground", () => ({
  useAppForeground: () => true,
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "x" }) }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    const wsId = queryKey[1] as string;
    if (queryKey.includes("sessions")) {
      return { data: data.sessions[wsId] ?? [], isSuccess: true };
    }
    if (queryKey[0] === "projects") {
      return { data: data.projects[wsId] ?? [], isSuccess: true };
    }
    if (queryKey.includes("draft-restores") || queryKey.includes("pending-task")) {
      return { data: null, isSuccess: true };
    }
    return { data: [], isSuccess: true };
  },
  useInfiniteQuery: () => ({
    data: undefined,
    isLoading: false,
    fetchNextPage: vi.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
  }),
}));

function session(id: string, workspaceId: string): ChatSession {
  return {
    id,
    workspace_id: workspaceId,
    creator_id: "user-1",
    agent_id: "agent-1",
    title: id,
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: "2026-09-28T00:00:00Z",
    updated_at: "2026-09-28T00:00:00Z",
  };
}

describe("useChatController workspace rehydration", () => {
  let storage: StorageAdapter;
  let store: ReturnType<typeof createChatStore>;
  let queryClient: QueryClient;
  const sourceSession = session("source-session", "ws-source");
  const targetSession = session("target-session", "ws-target");
  const targetProject = { id: "target-project", workspace_id: "ws-target" } as Project;

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  }

  beforeEach(async () => {
    route.workspaceId = "ws-source";
    route.isActive = true;
    data.sessions = { "ws-source": [sourceSession], "ws-target": [targetSession] };
    data.projects = { "ws-source": [], "ws-target": [targetProject] };
    await act(async () => setCurrentWorkspace("source", "ws-source"));
    const values = new Map<string, string>([
      ["multica:chat:activeSessionId:source", sourceSession.id],
      ["multica:chat:activeSessionId:target", targetSession.id],
      ["multica:chat:selectedProjectId:target", targetProject.id],
    ]);
    storage = {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => { values.set(key, value); },
      removeItem: (key) => { values.delete(key); },
    };
    store = createChatStore({ storage });
    registerChatStore(store);
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(async () => {
    cleanup();
    queryClient.clear();
    await act(async () => setCurrentWorkspace(null, null));
  });

  function expectTargetSelection() {
    expect(store.getState().activeSessionId).toBe(targetSession.id);
    expect(storage.getItem("multica:chat:activeSessionId:target")).toBe(targetSession.id);
    expect(store.getState().selectedProjectId).toBe(targetProject.id);
    expect(storage.getItem("multica:chat:selectedProjectId:target")).toBe(targetProject.id);
    expect(storage.getItem("multica:chat:activeSessionId:source")).toBe(sourceSession.id);
  }

  it.each([false, true])("preserves the target selection while the source controller remains mounted (isActive=%s)", async (isActive) => {
    const { result } = renderHook(() => useChatController({ isActive: route.isActive }), { wrapper });
    expect(result.current.currentSession?.id).toBe(sourceSession.id);

    await act(async () => {
      route.isActive = isActive;
      setCurrentWorkspace("target", "ws-target");
    });

    // The outgoing route still owns the source queries while the real store
    // has already read the target namespace through the rehydration callback.
    expectTargetSelection();
  });

  it("does not delete target storage when the workspace changes between render and effects", async () => {
    data.sessions["ws-source"] = [];
    function SwitchBeforeEffects({ children }: { children: ReactNode }) {
      useLayoutEffect(() => setCurrentWorkspace("target", "ws-target"), []);
      return wrapper({ children });
    }

    renderHook(() => useChatController({ isActive: true }), { wrapper: SwitchBeforeEffects });
    await act(async () => { await Promise.resolve(); });

    expectTargetSelection();
  });

  it("still clears deleted selections after the controller enters the target workspace", async () => {
    const { rerender } = renderHook(() => useChatController({ isActive: route.isActive }), { wrapper });
    await act(async () => {
      setCurrentWorkspace("target", "ws-target");
    });
    route.workspaceId = "ws-target";
    rerender();
    expectTargetSelection();

    data.sessions["ws-target"] = [];
    data.projects["ws-target"] = [];
    rerender();

    expect(store.getState().activeSessionId).toBeNull();
    expect(store.getState().selectedProjectId).toBeNull();
    expect(storage.getItem("multica:chat:activeSessionId:target")).toBeNull();
    expect(storage.getItem("multica:chat:selectedProjectId:target")).toBeNull();
    expect(storage.getItem("multica:chat:activeSessionId:source")).toBe(sourceSession.id);
  });
});

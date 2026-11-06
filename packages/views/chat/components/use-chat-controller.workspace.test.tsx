import { useLayoutEffect, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, screen, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { Agent, Attachment, Workspace, ChatSession, Project, StorageAdapter } from "@multica/core/types";
import { ChatWindow } from "./chat-window";
import { useChatController } from "./use-chat-controller";

const route = vi.hoisted(() => ({ workspaceId: "ws-source", isActive: true }));
const data = vi.hoisted(() => ({
  agents: [] as Agent[],
  workspaces: [] as Workspace[],
  agentsLoaded: true,
  membersLoaded: true,
  sessionsLoaded: true,
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
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => ({ availability: "online" }),
  useCustomizeConversationStartersHref: () => null,
  useWorkspaceAgentAvailability: () => "available",
}));
vi.mock("../../common/use-app-foreground", () => ({
  useAppForeground: () => true,
}));
vi.mock("../../i18n", () => ({ useLocale: () => "en", useT: () => ({ t: () => "x" }) }));
vi.mock("./chat-input", () => ({ ChatInput: ({ agentName }: { agentName?: string }) => <input data-testid="quick-agent" data-agent={agentName ?? "none"} /> }));
vi.mock("./chat-empty-state", () => ({ EmptyState: () => null }));
vi.mock("./chat-window-messages", () => ({ ChatWindowMessages: () => null }));
vi.mock("./use-chat-context-items", () => ({ useChatContextItems: () => [] }));
vi.mock("./use-chat-open-reveal", () => ({ useChatOpenReveal: () => ({ contentReady: true, markContentReady: () => {} }) }));
vi.mock("./use-chat-resize", () => ({ useChatResize: () => ({ renderWidth: 400, renderHeight: 500, boundsReady: true }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    const wsId = queryKey[1] as string;
    if (queryKey[0] === "workspaces" && queryKey[1] === "list") return { data: data.workspaces, isSuccess: true };
    if (queryKey.includes("agents")) return { data: data.agents, isSuccess: data.agentsLoaded };
    if (queryKey.includes("members")) return { data: [{ user_id: "user-1", role: "owner" }], isSuccess: data.membersLoaded };
    if (queryKey.includes("sessions")) {
      return { data: data.sessions[wsId] ?? [], isSuccess: data.sessionsLoaded };
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
    data.agents = [];
    data.workspaces = [];
    data.agentsLoaded = true;
    data.membersLoaded = true;
    data.sessionsLoaded = true;
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
  function prepareDefaults() {
    data.agents = ["first", "chosen", "other"].map((id) => ({ id, name: id, owner_id: "user-1", runtime_id: "runtime" } as Agent));
    data.workspaces = [
      { id: "ws-source", settings: { default_chat_agent_id: "chosen" } },
      { id: "ws-target", settings: { default_chat_agent_id: "other" } },
    ] as unknown as Workspace[];
    store.getState().setActiveSession(null);
    store.getState().setOpen(true);
    store.getState().setInputDraft(DRAFT_NEW_SESSION, "unsent text");
    store.getState().addInputDraftAttachment(DRAFT_NEW_SESSION, { id: "attachment", filename: "draft.txt" } as Attachment);
  }

  it("wires the same fallback into main Chat and quick-chat without changing drafts, preferences or sessions", () => {
    prepareDefaults();
    data.projects["ws-source"] = [{ id: "kept-project", workspace_id: "ws-source" } as Project];
    store.getState().setSelectedProjectId("kept-project");
    const draft = store.getState().inputDrafts;
    const attachments = store.getState().inputDraftAttachments;
    const { result, rerender } = renderHook(() => useChatController(), { wrapper });
    const window = render(<ChatWindow />, { wrapper });
    expect(result.current.activeAgent?.id).toBe("chosen");
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "chosen");
    screen.getByTestId("quick-agent").focus();
    data.workspaces[0] = { ...data.workspaces[0]!, settings: { default_chat_agent_id: "other" } };
    rerender(); window.rerender(<ChatWindow />);
    expect(screen.getByTestId("quick-agent")).toHaveFocus();
    expect(result.current.activeAgent?.id).toBe("other");
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "other");
    expect(store.getState().selectedAgentId).toBeNull();
    expect(store.getState().selectedProjectId).toBe("kept-project");
    expect(store.getState().activeSessionId).toBeNull();
    expect(store.getState().inputDrafts).toBe(draft);
    expect(store.getState().inputDraftAttachments).toBe(attachments);
    act(() => store.getState().setSelectedAgentId("chosen"));
    expect(result.current.activeAgent?.id).toBe("chosen");
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "chosen");
    data.sessions["ws-source"] = [session("live", "ws-source")];
    data.agents.push({ ...data.agents[0]!, id: "agent-1", name: "session agent" });
    act(() => store.getState().setActiveSession("live"));
    expect(result.current.activeAgent?.id).toBe("agent-1");
    const beforeURLSync = renderHook(() => useChatController({ intentSessionId: null }), { wrapper });
    expect(beforeURLSync.result.current.activeAgent?.id).toBe("agent-1");
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "session agent");
    data.workspaces[0] = { ...data.workspaces[0]!, settings: {} };
    rerender(); window.rerender(<ChatWindow />);
    expect(store.getState().activeSessionId).toBe("live");
    expect(result.current.activeAgent?.id).toBe("agent-1");
    expect(store.getState().inputDrafts).toBe(draft);
  });

  it("waits for member qualification and pending session/URL intent even when the default is already loaded", () => {
    prepareDefaults();
    data.membersLoaded = false;
    const { result, rerender } = renderHook(() => useChatController(), { wrapper });
    const window = render(<ChatWindow />, { wrapper });
    expect(result.current.activeAgent).toBeNull();
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "none");
    data.membersLoaded = true;
    data.sessionsLoaded = false;
    act(() => store.getState().setActiveSession("pending"));
    expect(result.current.activeAgent).toBeNull();
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "none");
    act(() => store.getState().setActiveSession(null));
    const explicit = renderHook(() => useChatController({ intentAgentId: "loading-url" }), { wrapper });
    expect(explicit.result.current.activeAgent).toBeNull();
    const pendingURLSession = renderHook(() => useChatController({ intentSessionId: "pending-url" }), { wrapper });
    expect(pendingURLSession.result.current.activeAgent).toBeNull();
    data.agentsLoaded = false;
    rerender(); window.rerender(<ChatWindow />);
    expect(result.current.activeAgent).toBeNull();
    expect(screen.getByTestId("quick-agent")).toHaveAttribute("data-agent", "none");
  });

  it("resolves workspace defaults in the current workspace and safely falls back after access loss", async () => {
    prepareDefaults();
    const { result, rerender } = renderHook(() => useChatController(), { wrapper });
    expect(result.current.activeAgent?.id).toBe("chosen");
    await act(async () => setCurrentWorkspace("target", "ws-target"));
    route.workspaceId = "ws-target";
    act(() => store.getState().setActiveSession(null));
    rerender();
    expect(result.current.activeAgent?.id).toBe("other");
    data.agents = data.agents.filter((agent) => agent.id !== "other");
    rerender();
    expect(result.current.activeAgent?.id).toBe("first");
    expect(store.getState().selectedAgentId).toBeNull();
  });

});

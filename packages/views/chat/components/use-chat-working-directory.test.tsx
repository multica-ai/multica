import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore } from "@multica/core/chat";
import { api } from "@multica/core/api";
import type { Agent, AgentRuntime, ChatSession } from "@multica/core/types";
import { useChatWorkingDirectory } from "./use-chat-working-directory";

vi.mock("@multica/core/platform", () => ({ getCurrentWsId: () => "ws" }));
vi.mock("@multica/core/api", () => ({ api: { listRuntimes: vi.fn() } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "user" } }) }));

const runtime = { workspace_id: "ws", name: "Codex", launch_header: "", device_info: "", visibility: "private", last_seen_at: null, created_at: "", updated_at: "", id: "runtime", daemon_id: "daemon", owner_id: "user", runtime_mode: "local", status: "online", provider: "codex", metadata: { capabilities: ["chat-working-directory-v1"] } } as AgentRuntime;
const agent = { id: "agent", runtime_id: "runtime", runtime_config: {} } as Agent;
const directory = { daemon_id: "daemon", path: "/home/me/项目 files" };
let store: ReturnType<typeof createChatStore>;
function renderDirectory(currentAgent = agent, session: ChatSession | null = null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  return renderHook(({ selectedAgent, currentSession }) => useChatWorkingDirectory("ws", selectedAgent, currentSession), { wrapper, initialProps: { selectedAgent: currentAgent, currentSession: session } });
}

beforeEach(() => {
  const memory = new Map<string, string>();
  store = createChatStore({ storage: { getItem: (k) => memory.get(k) ?? null, setItem: (k, v) => { memory.set(k, v); }, removeItem: (k) => { memory.delete(k); } } });
  registerChatStore(store);
  vi.mocked(api.listRuntimes).mockResolvedValue([runtime]);
});

describe("chat directory selection shared by both surfaces", () => {
  it("carries the selected path for a new chat and never reuses another agent's draft", async () => {
    const { result, rerender } = renderDirectory();
    await waitFor(() => expect(result.current.runtime?.id).toBe("runtime"));
    act(() => result.current.onChange(directory));
    expect(result.current.workingDirectory).toEqual(directory);
    rerender({ selectedAgent: { ...agent, id: "other-agent" }, currentSession: null });
    expect(result.current.workingDirectory).toBeNull();
  });
  it("starts a fresh conversation with the same agent and project when changing an existing path", async () => {
    const session = { id: "session", agent_id: "agent", project_id: "project", working_directory: directory } as ChatSession;
    store.getState().setSelectedAgentId("stale-agent");
    store.getState().setActiveSession("session");
    const { result } = renderDirectory(agent, session);
    await waitFor(() => expect(result.current.runtime?.id).toBe("runtime"));
    expect(result.current.workingDirectory).toEqual(directory);
    act(() => result.current.onChange({ ...directory, path: "/next" }));
    expect(store.getState().activeSessionId).toBeNull();
    expect(store.getState().selectedAgentId).toBe("agent");
    expect(store.getState().selectedProjectId).toBe("project");
    expect(store.getState().workingDirectoryDraft?.directory.path).toBe("/next");
  });
  it("drops a draft when the runtime moves to another daemon", async () => {
    store.getState().setWorkingDirectoryDraft({ agentId: "agent", runtimeId: "runtime", directory });
    vi.mocked(api.listRuntimes).mockResolvedValue([{ ...runtime, daemon_id: "new-daemon" }]);
    const { result } = renderDirectory();
    await waitFor(() => expect(result.current.runtime?.daemon_id).toBe("new-daemon"));
    expect(result.current.workingDirectory).toBeNull();
    expect(store.getState().workingDirectoryDraft).toBeNull();
  });
  it("uses persisted session selection for continuations instead of a draft", () => {
    store.getState().setWorkingDirectoryDraft({ agentId: "agent", runtimeId: "runtime", directory: { ...directory, path: "/wrong" } });
    const { result } = renderDirectory(agent, { id: "session", working_directory: directory } as ChatSession);
    expect(result.current.workingDirectory).toEqual(directory);
  });
  it.each([
    { ...runtime, owner_id: "other" },
    { ...runtime, runtime_mode: "cloud" },
    { ...runtime, daemon_id: null },
    { ...runtime, status: "offline" },
    { ...runtime, metadata: {} },
  ])("hides the selector for unsupported runtimes", async (row) => {
    vi.mocked(api.listRuntimes).mockResolvedValue([row] as AgentRuntime[]);
    const { result } = renderDirectory();
    await waitFor(() => expect(api.listRuntimes).toHaveBeenCalled());
    expect(result.current.runtime).toBeNull();
  });
  it("hides local directory selection for OpenClaw gateway agents", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([{ ...runtime, provider: "openclaw" }]);
    const { result } = renderDirectory({ ...agent, runtime_config: { mode: "gateway" } });
    await waitFor(() => expect(api.listRuntimes).toHaveBeenCalled());
    expect(result.current.runtime).toBeNull();
  });
});

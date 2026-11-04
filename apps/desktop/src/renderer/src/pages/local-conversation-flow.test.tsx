import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { localMessageDisplayContent, type LocalConversation, type LocalFolderProject, type LocalUserQuestion } from "@multica/core/local-conversation/protocol";
import type { Project } from "@multica/core/types";
import chat from "../../../../../../packages/views/locales/zh-Hans/chat.json";
import layout from "../../../../../../packages/views/locales/zh-Hans/layout.json";
import { LocalConversationFlowPage } from "./local-conversation-flow";
import { useProjectVisibilityStore } from "../stores/local-conversation-project-visibility";
import { useConversationLayoutStore } from "../stores/local-conversation-layout";
import type { TuttiAgentGuiPanel } from "../components/tutti-agent-gui-panel";
import type { ComponentProps } from "react";
import type { LocalHostAvailability } from "../../../shared/local-host-availability";
const transportFor = vi.hoisted(() => ({ calls: [] as string[] }));
vi.mock("../platform/local-v2-transport", () => ({ setRemoteFolderChooser: () => {}, localV2Transport: (deviceId = "local") => { transportFor.calls.push(deviceId); return window.desktopAPI.localV2; } }));
vi.mock("../platform/linked-devices", () => ({ useLinkedDevices: () => [{ peer_id: "peer-1", device_name: "Mac Studio", connection: "direct" }], deviceName: (_devices: unknown, id: string) => id === "peer-1" ? "Mac Studio" : id }));

const mock = await vi.hoisted(async () => {
  const { createContext, useContext } = await import("react");
  // Route and layout changes reach the page through context, as they do in the app.
  const Route = createContext(0);
  return { Route, useRoute: () => useContext(Route), request: vi.fn(), replace: vi.fn(), pathname: "/test/conversations", selected: "source", error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn(), panel: vi.fn(), copyText: vi.fn(), compact: false, locale: "zh-Hans", projects: [] as Project[], folders: [] as LocalFolderProject[], folderError: null as Error | null, folderPending: null as Promise<unknown> | null };
});
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: mock.copyText }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["test-projects"], queryFn: async () => mock.projects }) }));
vi.mock("sonner", () => ({ toast: { error: mock.error, success: mock.success, info: mock.info, warning: mock.warning } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (selector: (state: unknown) => unknown) => selector({ user: { id: "user" } }) }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "workspace" }), useRequiredWorkspaceSlug: () => "test", useWorkspacePaths: () => ({ conversationFlow: () => "/test/conversations", conversationFlowDevice: (id: string) => `/test/conversations/devices/${id}` }) }));
vi.mock("@multica/core/platform", () => ({ getCurrentSlug: () => "test" }));
vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => { mock.useRoute(); return { pathname: mock.pathname, searchParams: new URLSearchParams({ session: mock.selected }), replace: mock.replace }; },
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => <a href={href} {...props}>{children}</a>,
}));
vi.mock("@multica/views/i18n", () => ({ useLocale: () => mock.locale, useT: (namespace?: string) => ({ t: (selector: (resource: typeof chat) => string, options?: { title?: string; device?: string }) => selector((namespace === "layout" ? layout : chat) as typeof chat).replace("{{title}}", options?.title ?? "").replace("{{device}}", options?.device ?? "") }) }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsCompact: () => { mock.useRoute(); return mock.compact; } }));
vi.mock("@multica/views/runtimes/provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("../components/tutti-agent-gui-panel", () => ({ TuttiAgentGuiPanel: (props: ComponentProps<typeof TuttiAgentGuiPanel>) => {
  mock.panel(props);
  return <textarea aria-label="Conversation draft" data-new-conversation={JSON.stringify(props.newConversationRequest)} />;
} }));
vi.mock("../platform/local-conversation-service", () => ({ localProvidersQuery: (store: { request(method: string, path: string): Promise<unknown> }) => ({ queryKey: ["test-local-sessions", "providers"], queryFn: () => store.request("GET", "/providers") }), localFolderProjectsQuery: (store: { request(method: string, path: string): Promise<unknown> }) => ({ queryKey: ["test-local-sessions", "folder-projects"], queryFn: () => store.request("GET", "/folder-projects") }), localConversationKey: () => ["test-local-sessions"], localTargetId: (provider: string) => `local:${provider}`, createLocalConversationParentService: vi.fn() }));

let summarySeq = 0;
/** Publishes the list again, as the daemon does after any change. */
let publishList: () => Promise<void> = async () => {};
let publishMessage: (message: unknown) => void = () => {};
let publishAvailability: (value: LocalHostAvailability) => void = () => {};
let publishConnection: (scope: { userId: string; workspaceId: string }, connected: boolean) => void = () => {};
let daemonRequests: { method: string; path: string; body?: Record<string, unknown> }[] = [];
/** Reads of the conversation list. */
const listReads = () => daemonRequests.filter((entry) => entry.method === "GET" && entry.path.startsWith("/sessions?"));
/** The daemon's list row for a v1-shaped fixture. */
function summary(entry: LocalConversation) {
  const running = entry.turns.filter((turn) => turn.status === "running");
  return { conversation_id: entry.id, summary_seq: ++summarySeq, provider: entry.provider, title: entry.title, cwd: entry.cwd, project_id: entry.projectId ?? null, pinned: entry.pinned, archived: entry.archived,
    background_tasks: entry.turns.flatMap((turn) => turn.backgroundTasks ?? []).filter((task) => task.status === "running" && !task.ambient).length,
    status: running.length ? "running" : "idle", pending_interactions: running.flatMap((turn) => turn.questions ?? []).filter((question) => question.status === "pending").length,
    last_message_preview: localMessageDisplayContent(entry.messages.at(-1)), turn_count: Math.max(entry.turns.length, entry.messages.length ? 1 : 0),
    updated_at: `${entry.updatedAt.slice(0, 10)}T00:00:00Z`, created_at: `${entry.createdAt.slice(0, 10)}T00:00:00Z`,
    ...(entry.side ? { side_source_id: entry.side.sourceId, ...(entry.side.turnId ? { side_source_turn_id: entry.side.turnId } : {}), ...(entry.side.quote ? { side_quote: entry.side.quote } : {}) } : {}) };
}
const folderRow = (project: LocalFolderProject) => ({ id: project.id, title: project.title, path: project.path, created_at: project.createdAt });
/**
 * Serves the page's v2 routes from the cases' v1-shaped fixtures: mock.request
 * still answers listPage, read, update, fork and the folder actions.
 */
function installDaemon() {
  const v2Listeners = new Set<(scope: unknown, message: unknown) => void>();
  const availabilityListeners = new Set<(value: LocalHostAvailability) => void>();
  const connectionListeners = new Set<(scope: { userId: string; workspaceId: string }, connected: boolean) => void>();
  let availability: LocalHostAvailability = { state: "connected" };
  publishAvailability = (value) => { availability = value; availabilityListeners.forEach((listener) => listener(value)); };
  publishConnection = (scope, connected) => connectionListeners.forEach((listener) => listener(scope, connected));
  daemonRequests = [];
  const rows = async (scope: unknown): Promise<LocalConversation[]> => {
    const result = await mock.request(scope, "listPage", {});
    return Array.isArray(result) ? result : result?.sessions ?? [];
  };
  const read = async (scope: unknown, id: string): Promise<LocalConversation> => {
    const result = await mock.request(scope, "read", { id });
    const entry = result && !Array.isArray(result) ? result : (await rows(scope)).find((row) => row.id === id);
    if (!entry) throw new Error("not_found: Local conversation not found");
    return entry;
  };
  const fail = (error: unknown): never => { throw error instanceof Error && /^[a-z_0-9]+: /.test(error.message) ? error : new Error(`internal: ${error instanceof Error ? error.message : String(error)}`); };
  const refresh = async (scope: unknown) => {
    const summaries = (await rows(scope)).map(summary);
    v2Listeners.forEach((listener) => listener(scope, { version: 2, type: "scope.summaries", scope_seq: summarySeq, summaries }));
  };
  publishList = () => refresh({ userId: "user", workspaceId: "workspace" });
  publishMessage = (message) => v2Listeners.forEach((listener) => listener({ userId: "user", workspaceId: "workspace" }, message));
  const localV2 = {
    request: async (scope: unknown, method: string, path: string, body?: Record<string, unknown>) => {
      daemonRequests.push({ method, path, body });
      try {
        const url = new URL(path, "http://daemon");
        if (path === "/providers") return { providers: ((await mock.request(scope, "providers", {})) ?? []).map((entry: { id: string; name?: string; available: boolean }) => ({ name: entry.id, ...entry })) };
        if (path === "/folder-projects") { if (mock.folderPending) await mock.folderPending; if (mock.folderError) throw mock.folderError; return { projects: mock.folders.map(folderRow) }; }
        if (url.pathname === "/sessions" && method === "GET") return { scope_seq: summarySeq, summaries: (await rows(scope)).map(summary), has_more: false };
        const match = /^\/sessions\/([^/]+)\/(snapshot|commands)$/.exec(url.pathname);
        if (match?.[2] === "snapshot") { const entry = await read(scope, match[1]!); return { version: 2, store_generation: "g", snapshot_seq: 0, conversation: summary(entry), turns: [] }; }
        if (match && body?.type === "update") {
          const { type: _type, project_id, ...fields } = body;
          const result = await mock.request(scope, "update", { id: match[1], ...fields, ...(project_id !== undefined ? { projectId: project_id } : {}) });
          const changed = summary(result ?? { ...await read(scope, match[1]!), ...fields });
          void refresh(scope); return changed;
        }
        if (match && body?.type === "fork") { const forked = summary(await mock.request(scope, "fork", { id: match[1] })); void refresh(scope); return forked; }
        if (match && body?.type === "stop_background_tasks") { await mock.request(scope, "stopBackgroundTasks", { id: match[1] }); void refresh(scope); return { status: "requested" }; }
      } catch (error) { fail(error); }
      throw new Error(`http_404: ${method} ${path}`);
    },
    choose: async (scope: unknown, choice: { purpose: string; conversationId?: string }) => {
      try {
        if (choice.purpose === "folder_project") { const project = await mock.request(scope, "folderProjectAdd", {}); return project && folderRow(project); }
        if (choice.purpose === "conversation_directory") return summary(await mock.request(scope, "directory", { id: choice.conversationId }) ?? await read(scope, choice.conversationId!));
      } catch (error) { fail(error); }
      return null;
    },
    send: async () => {}, onConnection: (listener: (scope: { userId: string; workspaceId: string }, connected: boolean) => void) => { connectionListeners.add(listener); return () => { connectionListeners.delete(listener); }; }, fileUrl: (_scope: unknown, path: string) => `v2:${path}`, upload: vi.fn(),
    availability: () => availability, onAvailability: (listener: (value: LocalHostAvailability) => void) => { availabilityListeners.add(listener); return () => { availabilityListeners.delete(listener); }; },
    subscribe: (listener: (scope: unknown, message: unknown) => void) => {
      v2Listeners.add(listener);
      return () => v2Listeners.delete(listener);
    },
  };
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { localV2, pickDirectory: vi.fn() } });
}

let qc: QueryClient;
let source: LocalConversation;
beforeEach(() => {
  useConversationLayoutStore.setState({ sessionListWidth: 268 });
  useProjectVisibilityStore.setState({ hiddenByScope: {} });
  vi.clearAllMocks(); mock.selected = "source"; mock.pathname = "/test/conversations";
  mock.compact = false; mock.locale = "zh-Hans";
  mock.projects = [];
  mock.folders = [];
  mock.folderError = null;
  mock.folderPending = null;
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  source = { id: "source", title: "Original", provider: "codex", cwd: "/owned/work", providerSessionId: "native", model: null, pinned: false, archived: false, createdAt: "2026-01-01", updatedAt: "2026-01-01", messages: [{ id: "message", role: "assistant", content: "Result", turnId: "turn", createdAt: "2026-01-01" }], turns: [] };
  mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source] : method === "providers" ? [{ id: "codex", available: true }] : undefined);
  installDaemon();
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
});
afterEach(() => { cleanup(); qc.clear(); useProjectVisibilityStore.setState({ hiddenByScope: {} }); vi.unstubAllGlobals(); vi.restoreAllMocks(); localStorage.removeItem("multica_local_conversation_width"); localStorage.clear(); });
/** IDs the list currently shows, in its order. */
function listedIds() {
  return screen.queryAllByRole("link").map((link) => new URL(link.getAttribute("href")!, "http://app").searchParams.get("session"));
}
let route = 0;
const page = () => <mock.Route.Provider value={++route}><QueryClientProvider client={qc}><LocalConversationFlowPage /></QueryClientProvider></mock.Route.Provider>;
const folder = (id: string, title: string, path = "/owned/work"): LocalFolderProject => ({ id, title, path, createdAt: "2026-01-01T00:00:00Z" });
/**
 * A menu trigger takes clicks once its effects have run, which it shows by gaining
 * aria-expanded. A query can return before then on a loaded machine, and that click
 * would do nothing.
 */
async function clickMenuTrigger(trigger: HTMLElement) {
  await waitFor(() => expect(trigger).toHaveAttribute("aria-expanded"));
  fireEvent.click(trigger);
}

async function activateWithPointer(element: HTMLElement) {
  await waitFor(() => expect(element).toHaveAttribute("aria-expanded"));
  const pointerDown = new MouseEvent("pointerdown", { bubbles: true, button: 0 });
  Object.defineProperty(pointerDown, "pointerType", { value: "mouse" });
  fireEvent(element, pointerDown);
  fireEvent.pointerUp(element, { pointerType: "mouse", button: 0 });
  fireEvent.click(element, { button: 0, detail: 1 });
}

describe("provider detection recovery", () => {
  it("shows detection in progress and lets the user retry a failed check", async () => {
    const original = mock.request.getMockImplementation()!;
    let rejectDetection: (error: Error) => void = () => {};
    const detection = new Promise((_resolve, reject) => { rejectDetection = reject; });
    mock.request.mockImplementation((scope, method, params) => method === "providers" ? detection : original(scope, method, params));
    render(page());
    const createButton = screen.getByRole("button", { name: "新建会话" });
    expect(createButton).toHaveAccessibleDescription("正在检测本机运行器…");
    await act(async () => rejectDetection(new Error("daemon_offline: Runtime starting")));
    const failure = await screen.findByRole("status", { name: "运行器检测失败" });
    mock.request.mockImplementation(original);
    fireEvent.click(within(failure).getByRole("button", { name: "重试" }));
    await waitFor(() => expect(createButton).not.toHaveAttribute("aria-disabled", "true"));
    fireEvent.click(createButton);
    expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex");
  });

  it("recovers provider and folder detection after the runtime connects without replacing the draft", async () => {
    const original = mock.request.getMockImplementation()!;
    let offline = true;
    mock.request.mockImplementation((scope, method, params) => method === "providers" && offline ? Promise.reject(new Error("daemon_offline: Runtime starting")) : original(scope, method, params));
    mock.folderError = new Error("daemon_offline: Runtime starting");
    publishAvailability({ state: "unavailable", reason: "daemon_offline" });
    render(page());
    expect(await screen.findByRole("status", { name: "运行器检测失败" })).toBeVisible();
    const createButton = screen.getByRole("button", { name: "新建会话" });
    expect(createButton).toHaveAttribute("aria-disabled", "true");
    expect(createButton).toHaveAccessibleDescription("运行器检测失败");
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });
    offline = false; mock.folderError = null;
    await act(async () => publishAvailability({ state: "connected" }));
    await waitFor(() => expect(createButton).not.toHaveAttribute("aria-disabled", "true"));
    expect(screen.queryByRole("status", { name: "运行器检测失败" })).not.toBeInTheDocument();
    expect(screen.queryByText("本地项目加载失败")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(draft);
    expect(draft).toHaveValue("Keep my draft");
  });

  it("rechecks availability on a reconnect of this scope and ignores other workspaces", async () => {
    const original = mock.request.getMockImplementation()!;
    let available = false;
    mock.request.mockImplementation((scope, method, params) => method === "providers" ? Promise.resolve([{ id: "codex", available }]) : original(scope, method, params));
    qc.setDefaultOptions({ queries: { retry: false, staleTime: Infinity } });
    render(page());
    const createButton = await screen.findByRole("button", { name: "新建会话" });
    await waitFor(() => expect(createButton).toHaveAccessibleDescription("请先在本机安装 Codex 或 Claude Code"));
    available = true;
    const reads = () => daemonRequests.filter((entry) => entry.path === "/providers").length;
    const before = reads();
    await act(async () => publishConnection({ userId: "user", workspaceId: "different" }, true));
    expect(reads()).toBe(before);
    await act(async () => publishConnection({ userId: "user", workspaceId: "workspace" }, true));
    await waitFor(() => expect(createButton).not.toHaveAttribute("aria-disabled", "true"));
    expect(reads()).toBeGreaterThan(before);
  });

  it("recovers the first conversation list after the daemon becomes available before any socket connects", async () => {
    const original = mock.request.getMockImplementation()!;
    let offline = true;
    mock.request.mockImplementation((scope, method, params) => offline && (method === "listPage" || method === "providers")
      ? Promise.reject(new Error("daemon_offline: Runtime starting")) : original(scope, method, params));
    mock.folderError = new Error("daemon_offline: Runtime starting");
    publishAvailability({ state: "unavailable", reason: "daemon_offline" });
    render(page());
    const list = within(screen.getByTestId("local-conversation-sessions"));
    expect(await list.findByRole("alert")).toBeVisible();
    expect(await screen.findByRole("status", { name: "运行器检测失败" })).toBeVisible();
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });

    offline = false;
    mock.folderError = null;
    await act(async () => publishAvailability({ state: "connected" }));

    expect(await screen.findByRole("link", { name: /Original/ })).toBeVisible();
    expect(list.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("status", { name: "运行器检测失败" })).not.toBeInTheDocument();
    expect(screen.queryByText("本地项目加载失败")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(draft);
    expect(draft).toHaveValue("Keep my draft");
  });
});

describe("reading only what the page needs", () => {
  const snapshotReads = () => daemonRequests.filter((entry) => entry.path.endsWith("/snapshot"));

  it("lists conversations while folder projects are still loading", async () => {
    mock.folderPending = new Promise(() => {});
    const other = { ...source, id: "other", title: "Not selected" };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, other] : method === "providers" ? [{ id: "codex", available: true }] : undefined);
    render(page());
    expect(await screen.findByRole("link", { name: /Not selected/ })).toBeVisible();
  });

  it("does not read the snapshot of a conversation the list already knows", async () => {
    render(page());
    expect(await screen.findByRole("link", { name: /Original/ })).toBeVisible();
    await waitFor(() => expect(listReads().length).toBeGreaterThan(0));
    expect(snapshotReads()).toEqual([]);
  });

  it("reads the snapshot of a selected conversation the list does not know", async () => {
    mock.selected = "elsewhere";
    mock.request.mockImplementation(async (_scope, method, params) => method === "listPage" ? [source]
      : method === "providers" ? [{ id: "codex", available: true }]
      : method === "read" && (params as { id: string }).id === "elsewhere" ? { ...source, id: "elsewhere" } : undefined);
    render(page());
    await waitFor(() => expect(snapshotReads().map((entry) => entry.path)).toEqual(["/sessions/elsewhere/snapshot"]));
  });
});

describe("Device link", () => {
  it("shows another device's conversations under its own route and title", async () => {
    mock.pathname = "/test/conversations/devices/peer-1"; mock.selected = "";
    transportFor.calls = [];
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source] : []);
    render(page());
    const list = await screen.findByTestId("local-conversation-sessions");
    await waitFor(() => expect(within(list).getAllByRole("link").length).toBeGreaterThan(0));
    expect(transportFor.calls).toContain("peer-1");
    expect(transportFor.calls).not.toContain("local");
    expect(within(list).getAllByRole("link")[0]).toHaveAttribute("href", expect.stringMatching(/^\/test\/conversations\/devices\/peer-1\?/));
    expect(document.title).toBe("AI 会话（Mac Studio）");
  });

  it("marks this device's conversations another device has open", async () => {
    mock.selected = "";
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source] : []);
    render(page());
    const list = await screen.findByTestId("local-conversation-sessions");
    await waitFor(() => expect(within(list).getAllByRole("link").length).toBeGreaterThan(0));
    const presence = (devices: { peer_id: string; device_name: string }[]) => act(async () => { publishMessage({ version: 2, type: "scope.presence", conversation_id: source.id, devices }); });
    await presence([{ peer_id: "peer-1", device_name: "MacBook" }]);
    expect(within(list).getByRole("status", { name: new RegExp(`^MacBook 正在使用：`) })).toHaveAttribute("title", "MacBook 正在使用");
    await presence([]);
    expect(within(list).queryByRole("status", { name: /正在使用/ })).not.toBeInTheDocument();
  });
});

describe("project conversation pagination", () => {
  function rows(count = 17) { return Array.from({ length: count }, (_, index) => ({ ...source, id: `row-${index}`, title: `Paged conversation ${index}` })); }
  it("loads 8 then 8 then 1 only on clicks, keeps loaded pages through refresh, and ignores scroll", async () => {
    mock.selected = "";
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? rows() : []);
    render(page());
    const list = await screen.findByTestId("local-conversation-sessions");
    await waitFor(() => expect(within(list).getAllByRole("link")).toHaveLength(8));
    expect(listReads()).toHaveLength(1);
    fireEvent.scroll(list, { target: { scrollTop: 10000 } });
    expect(listReads()).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await waitFor(() => expect(within(list).getAllByRole("link")).toHaveLength(16));
    await act(async () => { await qc.invalidateQueries({ queryKey: ["test-local-sessions", "pages"] }); });
    expect(within(list).getAllByRole("link")).toHaveLength(16);
    // Pages show more of the list the daemon already sent.
    expect(listReads()).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await waitFor(() => expect(within(list).getAllByRole("link")).toHaveLength(17));
    expect(screen.queryByRole("button", { name: "加载更多" })).not.toBeInTheDocument();
    expect(mock.request.mock.calls.some((call) => ["list", "read", "fileInfo", "fileRead", "imageRead"].includes(call[1]))).toBe(false);
  });
  it("offers a retry when the list cannot be read and keeps showing more after it loads", async () => {
    mock.selected = "";
    let fail = true;
    mock.request.mockImplementation(async (_scope, method) => {
      if (method !== "listPage") return [];
      if (fail) throw new Error("internal: list unavailable");
      return rows(9);
    });
    render(page());
    const retry = await screen.findByRole("button", { name: "重试" });
    fail = false;
    fireEvent.click(retry);
    const more = await screen.findByRole("button", { name: "加载更多" });
    expect(screen.getAllByRole("link")).toHaveLength(8);
    fireEvent.click(more); fireEvent.click(more);
    await waitFor(() => expect(screen.getAllByRole("link")).toHaveLength(9));
    expect(listReads()).toHaveLength(2);
  });
  it("keeps project pagination independent and searches beyond the loaded page", async () => {
    mock.selected = "";
    mock.folders = [folder("a", "Project A"), folder("b", "Project B", "/other")];
    const entries = [...rows(), ...rows(9).map((entry) => ({ ...entry, id: `b-${entry.id}`, cwd: "/other" }))];
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? entries : []);
    render(page());
    const a = within(await screen.findByRole("region", { name: "Project A" }));
    const b = within(screen.getByRole("region", { name: "Project B" }));
    expect(a.getAllByRole("link")).toHaveLength(8); expect(b.getAllByRole("link")).toHaveLength(8);
    fireEvent.click(a.getByRole("button", { name: "加载更多" }));
    await waitFor(() => expect(a.getAllByRole("link")).toHaveLength(16));
    expect(b.getAllByRole("link")).toHaveLength(8);
    fireEvent.change(screen.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Paged conversation 16" } });
    expect(await screen.findByRole("link", { name: /Paged conversation 16/ })).toBeVisible();
    expect(screen.queryByRole("button", { name: "加载更多" })).not.toBeInTheDocument();
  });
  it("opens a deep link outside the first page without loading intervening pages or clearing selection", async () => {
    const entries = rows(); mock.selected = entries[16]!.id;
    mock.request.mockImplementation(async (_scope, method, params) => method === "listPage" ? entries : method === "read" ? entries.find((entry) => entry.id === params.id) : []);
    render(page());
    expect(await screen.findByText("Paged conversation 16")).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByRole("link")).toHaveLength(8));
    expect(mock.replace).not.toHaveBeenCalled();
    expect(listReads()).toHaveLength(1);
  });
});

describe("restoring the selected conversation after a tab switch", () => {
  it("reloads a failed remote selection when that device reconnects without clearing the route", async () => {
    mock.pathname = "/test/conversations/devices/peer-1";
    const original = mock.request.getMockImplementation()!;
    let online = false;
    mock.request.mockImplementation(async (scope, method, params) => {
      if (method === "listPage") return [];
      if (method === "read") {
        if (!online) throw new Error("device_unreachable: the network changed");
        return source;
      }
      return original(scope, method, params);
    });
    render(page());
    const reads = () => mock.request.mock.calls.filter(([, method]) => method === "read").length;
    await waitFor(() => expect(reads()).toBeGreaterThan(0));
    const before = reads(); online = true;
    await act(async () => publishConnection({ userId: "user", workspaceId: "different" }, true));
    expect(reads()).toBe(before);
    await act(async () => publishConnection({ userId: "user", workspaceId: "workspace" }, true));
    expect(await screen.findByRole("link", { name: /Original/ })).toBeVisible();
    expect(reads()).toBeGreaterThan(before);
    expect(mock.replace).not.toHaveBeenCalled();
  });

  it("recovers the conversation list error after reconnecting", async () => {
    const original = mock.request.getMockImplementation()!;
    let online = false;
    mock.request.mockImplementation(async (scope, method, params) => {
      if (method === "listPage" && !online) throw new Error("device_unreachable: the network changed");
      return original(scope, method, params);
    });
    render(page());
    const list = screen.getByTestId("local-conversation-sessions");
    await within(list).findByRole("button", { name: "重试" });
    online = true;
    await act(async () => publishConnection({ userId: "user", workspaceId: "workspace" }, true));
    expect(await within(list).findByRole("link", { name: /Original/ })).toBeVisible();
    expect(within(list).queryByRole("button", { name: "重试" })).not.toBeInTheDocument();
  });

  it("leaves image markers out of local session previews without changing saved history", async () => {
    source.messages = [{ id: "image-message", role: "user", content: "Can you see it?\n\n[Image attachment: screen.png]", attachmentIds: ["image"], turnId: "turn", createdAt: source.createdAt }];
    render(page());
    const entry = await screen.findByRole("link", { name: /Original/ });
    expect(entry).toHaveTextContent("Codex · Can you see it?");
    expect(entry).not.toHaveTextContent("Image attachment");
    expect(source.messages[0]?.content).toContain("[Image attachment:");
  });
  it("does not clear selection against a stale list while the restored tab refreshes", async () => {
    qc.setQueryDefaults(["test-local-sessions"], { staleTime: Infinity });
    qc.setQueryData(["test-local-sessions"], [source]);
    const restored = { ...source, id: "restored-session", title: "Previously selected conversation", provider: "claude" as const };
    mock.selected = restored.id;
    let resolve!: (entries: LocalConversation[]) => void;
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? new Promise<LocalConversation[]>((done) => { resolve = done; }) : []);
    render(page());
    await waitFor(() => expect(mock.request).toHaveBeenCalledWith(expect.anything(), "listPage", {}));
    expect(mock.replace).not.toHaveBeenCalled();
    await act(async () => { resolve([source, restored]); });
    expect(await screen.findByRole("link", { name: /Previously selected conversation/ })).toHaveAttribute("aria-current", "true");
    expect(mock.replace).not.toHaveBeenCalled();
  });

  it("leaves another tab's session alone while the conversation page unmounts and restores its original item on return", async () => {
    const view = render(page());
    expect(await screen.findByRole("link", { name: /Original/ })).toHaveAttribute("aria-current", "true");
    mock.pathname = "/test/chat"; mock.selected = "other-tab-chat-session";
    view.rerender(page());
    expect(mock.replace).not.toHaveBeenCalled();
    mock.pathname = "/test/conversations"; mock.selected = source.id;
    view.rerender(page());
    expect(screen.getByRole("link", { name: /Original/ })).toHaveAttribute("aria-current", "true");
    expect(mock.replace).not.toHaveBeenCalled();
  });

  it("still clears a missing conversation after the active tab's fresh list has loaded", async () => {
    mock.selected = "deleted-session";
    render(page());
    await waitFor(() => expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex"));
  });
});

describe("local conversation execution indicators", () => {
  const question = (request_id: string): LocalUserQuestion => ({ request_id, status: "pending", createdAt: "2026-01-01", updatedAt: "2026-01-01", questions: [{ id: "choice", question: "Which approach?", options: [{ label: "Simple" }, { label: "Detailed" }], multiSelect: false, allowFreeText: true }] });

  it.each(["codex", "claude"] as const)("shows pending %s questions in an unselected row and updates after answers and cancellation", async (provider) => {
    source = { ...source, provider, turns: [{ id: "question-turn", status: "running", createdAt: "2026-01-01", events: [], questions: [question("first"), question("second")] }] };
    const other = { ...source, id: "other", title: "Other conversation", turns: [] };
    mock.selected = other.id;
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [other, source] : []);
    // The daemon publishes the list after a conversation changes.
    const notify = (_scope: unknown, _id: string) => { void publishList(); };
    const mounted = render(page());
    const warning = await screen.findByRole("button", { name: "等待你的回答：Original" });
    const row = screen.getByRole("link", { name: /Original/ });
    expect(row).not.toHaveAttribute("aria-current");
    expect(row).toHaveTextContent("等待你的回答");
    expect(screen.queryByRole("status", { name: "会话运行中：Original" })).not.toBeInTheDocument();
    fireEvent.click(warning);
    expect(mock.replace).toHaveBeenCalledWith(`/test/conversations?provider=${provider}&session=source`);

    const updateQuestions = async (statuses: LocalUserQuestion["status"][]) => {
      source = { ...source, turns: source.turns.map((turn) => ({ ...turn, questions: turn.questions!.map((item, index) => ({ ...item, status: statuses[index]! })) })) };
      act(() => notify({ userId: "user", workspaceId: "workspace" }, source.id));
      // The daemon publishes the changed row; wait until the page shows it.
      await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    };
    await updateQuestions(["answered", "pending"]);
    expect(screen.getByRole("button", { name: "等待你的回答：Original" })).toBeVisible();
    await updateQuestions(["answered", "answered"]);
    expect(await screen.findByRole("status", { name: "会话运行中：Original" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "等待你的回答：Original" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Original/ })).toHaveTextContent(`${provider === "codex" ? "Codex" : "Claude Code"} · Result`);
    await updateQuestions(["answered", "pending"]);
    expect(screen.getByRole("button", { name: "等待你的回答：Original" })).toBeVisible();
    await updateQuestions(["answered", "cancelled"]);
    expect(screen.queryByRole("button", { name: "等待你的回答：Original" })).not.toBeInTheDocument();

    mounted.unmount();
    render(page());
    expect(await screen.findByRole("status", { name: "会话运行中：Original" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "等待你的回答：Original" })).not.toBeInTheDocument();
  });

  it.each(["completed", "failed", "cancelled"] as const)("does not warn about stale pending questions in a %s turn", async (status) => {
    source.turns = [{ id: "old-turn", status, createdAt: "2026-01-01", events: [], questions: [question("stale")], backgroundTasks: [{ id: "watcher", description: "Monitor progress", status: "running" }] }];
    render(page());
    expect(await screen.findByRole("status", { name: "后台运行中：Original（1）" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "等待你的回答：Original" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Original/ })).toHaveTextContent("Codex · Result");
  });

  it("distinguishes background monitoring from a foreground reply and can stop it from the session menu", async () => {
    source = { ...source, provider: "claude", turns: [{ id: "finished-turn", status: "completed", createdAt: "2026-01-01", events: [], backgroundTasks: [{ id: "watcher", description: "Wait for PR to merge", status: "running" }] }] };
    mock.request.mockImplementation(async (_scope, method) => {
      if (method === "stopBackgroundTasks") { source = { ...source, turns: [] }; return undefined; }
      return method === "listPage" ? [source] : [];
    });
    render(page());
    expect(await screen.findByRole("status", { name: "后台运行中：Original（1）" })).toBeVisible();
    expect(screen.queryByRole("status", { name: "会话运行中：Original" })).not.toBeInTheDocument();
    await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Original" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "停止后台任务" }));
    await waitFor(() => expect(screen.queryByRole("status", { name: "后台运行中：Original（1）" })).not.toBeInTheDocument());
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "stopBackgroundTasks", { id: "source" });
  });
  it.each([
    ["codex", "completed"], ["codex", "failed"], ["codex", "cancelled"],
    ["claude", "completed"], ["claude", "failed"], ["claude", "cancelled"],
  ] as const)("updates the %s row from idle through running to %s after execution notifications", async (provider, status) => {
    source = { ...source, provider };
    // The daemon publishes the list after a conversation changes.
    const notify = (_scope: unknown, _id: string) => { void publishList(); };
    render(page());
    await screen.findByRole("link", { name: /Original/ });
    expect(screen.queryByRole("status", { name: "会话运行中：Original" })).not.toBeInTheDocument();

    source = { ...source, turns: [{ id: "active-turn", status: "running", createdAt: "2026-01-01", events: [] }] };
    act(() => notify({ userId: "user", workspaceId: "workspace" }, source.id));
    expect(await screen.findByRole("status", { name: "会话运行中：Original" })).toBeVisible();

    source = { ...source, turns: source.turns.map((turn) => ({ ...turn, status })) };
    act(() => notify({ userId: "user", workspaceId: "workspace" }, source.id));
    await waitFor(() => expect(screen.queryByRole("status", { name: "会话运行中：Original" })).not.toBeInTheDocument());
  });

  it("shows background execution independently of selection and keeps its actions available until all turns settle", async () => {
    let background: LocalConversation = { ...source, id: "background", title: "Background conversation", turns: [
      { id: "first-agent", status: "running", createdAt: "2026-01-01", events: [], remote: { agentId: "a", agentName: "First agent" } },
      { id: "second-agent", status: "running", createdAt: "2026-01-01", events: [], remote: { agentId: "b", agentName: "Second agent" } },
    ] };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, background] : []);
    // The daemon publishes the list after a conversation changes.
    const notify = (_scope: unknown, _id: string) => { void publishList(); };
    render(page());
    expect(await screen.findByRole("status", { name: "会话运行中：Background conversation" })).toBeVisible();
    expect(screen.getByRole("link", { name: /Original/ })).toHaveAttribute("aria-current", "true");
    expect(screen.queryByRole("status", { name: "会话运行中：Original" })).not.toBeInTheDocument();
    const menu = screen.getByRole("button", { name: "会话操作：Background conversation" });
    expect(menu).toBeEnabled();
    await clickMenuTrigger(menu);
    expect(await screen.findByRole("menuitem", { name: "复制会话 ID" })).toBeVisible();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    background = { ...background, turns: background.turns.slice(1) };
    act(() => notify({ userId: "user", workspaceId: "workspace" }, background.id));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(screen.getByRole("status", { name: "会话运行中：Background conversation" })).toBeVisible();
    background = { ...background, turns: [] };
    act(() => notify({ userId: "user", workspaceId: "workspace" }, background.id));
    await waitFor(() => expect(screen.queryByRole("status", { name: "会话运行中：Background conversation" })).not.toBeInTheDocument());
    expect(mock.replace).not.toHaveBeenCalled();
  });
});

describe("removing projects from the local conversation list", () => {
  it("opens project actions for a pointer activation without compatibility mouse events", async () => {
    mock.folders = [folder("project-a", "Pointer project")];
    render(page());
    const group = await screen.findByRole("region", { name: "Pointer project" });
    const trigger = within(group).getByRole("button", { name: "项目操作：Pointer project" });
    await activateWithPointer(trigger);
    expect(await screen.findByRole("menuitem", { name: "从列表移除" })).toBeVisible();
  });
  it("hides a project without mutating its conversations or replacing the active draft, and restores it from the list", async () => {
    mock.folders = [folder("project-a", "Removable project")];
    const original = structuredClone(source);
    render(page());
    const group = await screen.findByRole("region", { name: "Removable project" });
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep this draft" } });
    const projectMenu = within(group).getByRole("button", { name: "项目操作：Removable project" });
    const pointerDown = new MouseEvent("pointerdown", { bubbles: true, button: 0 });
    Object.defineProperty(pointerDown, "pointerType", { value: "mouse" });
    fireEvent(projectMenu, pointerDown);
    fireEvent.mouseDown(projectMenu, { button: 0 });
    await act(async () => { await new Promise((resolve) => requestAnimationFrame(resolve)); });
    fireEvent.pointerUp(projectMenu, { pointerType: "mouse", button: 0 });
    fireEvent.mouseUp(projectMenu, { button: 0 });
    fireEvent.click(projectMenu, { button: 0, detail: 1 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "从列表移除" }));
    await waitFor(() => expect(screen.queryByRole("region", { name: "Removable project" })).not.toBeInTheDocument());
    expect(source).toEqual(original);
    expect(mock.request.mock.calls.every((call) => ["listPage", "read", "providers", "sideWorkspaceSync"].includes(call[1]))).toBe(true);
    expect(mock.replace).not.toHaveBeenCalled();
    expect(draft).toHaveValue("Keep this draft");
    fireEvent.change(screen.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Original" } });
    expect(screen.queryByRole("region", { name: "Removable project" })).not.toBeInTheDocument();
    await activateWithPointer(screen.getByRole("button", { name: "已移除项目（1）" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "恢复项目：Removable project" }));
    expect(await screen.findByRole("region", { name: "Removable project" })).toBeVisible();
    expect(screen.getByRole("link", { name: /Original/ })).toBeVisible();
    expect(screen.queryByRole("button", { name: "已移除项目（1）" })).not.toBeInTheDocument();
  });
  it("keeps a removed empty project hidden after remounting and supports undo", async () => {
    mock.folders = [folder("project-a", "Empty project", "/owned/empty")];
    const mounted = render(page());
    const group = await screen.findByRole("region", { name: "Empty project" });
    await clickMenuTrigger(within(group).getByRole("button", { name: "项目操作：Empty project" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "从列表移除" }));
    const notification = mock.success.mock.calls.at(-1)!;
    mounted.unmount();
    render(page());
    expect(await screen.findByRole("button", { name: "已移除项目（1）" })).toBeVisible();
    expect(screen.queryByRole("region", { name: "Empty project" })).not.toBeInTheDocument();
    await act(async () => { notification[1].action.onClick(); });
    expect(await screen.findByRole("region", { name: "Empty project" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "项目操作：未分配项目" })).not.toBeInTheDocument();
  });
});

describe("local conversation project creation", () => {
  it("explains a stale desktop process for both folder loading and adding instead of reporting a generic project failure", async () => {
    // A daemon without the local v2 API.
    const error = new Error("http_404: Local conversation request failed (HTTP 404)");
    mock.folderError = error;
    mock.request.mockImplementation(async (_scope, method) => {
      if (method === "folderProjectAdd") throw error;
      return method === "listPage" ? [source] : [{ id: "codex", available: true }];
    });
    render(page());
    expect(await screen.findByText("本地项目功能已更新，请重启桌面应用")).toBeVisible();
    expect(screen.queryByText("本地项目加载失败")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "新建项目" }));
    await waitFor(() => expect(mock.error).toHaveBeenCalledWith("本地项目功能已更新，请重启桌面应用"));
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("adds a chosen local folder without leaving the conversation and uses it for new sessions", async () => {
    mock.projects = [{ id: "cloud-project", workspace_id: "workspace", title: "Cloud task project" }] as Project[];
    mock.request.mockImplementation(async (_scope, method) => {
      if (method === "folderProjectAdd") { const project = folder("created-project", "local-repository", "/owned/local-repository"); mock.folders = [project]; return project; }
      return method === "listPage" ? [source] : [{ id: "codex", available: true }];
    });
    render(page());
    await screen.findByRole("link", { name: /Original/ });
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });
    fireEvent.change(screen.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Original" } });
    fireEvent.click(screen.getByRole("button", { name: "新建项目" }));
    const group = await screen.findByRole("region", { name: "local-repository" });
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "folderProjectAdd", {});
    expect(mock.replace).not.toHaveBeenCalled();
    expect(draft).toHaveValue("Keep my draft");
    expect(screen.getByRole("textbox", { name: "搜索会话" })).toHaveValue("");
    expect(within(group).getByText("暂无会话")).toBeVisible();
    expect(within(group).getByRole("button", { name: "local-repository" })).toHaveAttribute("title", "/owned/local-repository");
    expect(screen.queryByRole("region", { name: "Cloud task project" })).not.toBeInTheDocument();
    fireEvent.click(within(group).getByRole("button", { name: "新建项目会话：local-repository" }));
    expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex");
    expect(JSON.parse(draft.getAttribute("data-new-conversation")!)).toMatchObject({ projectPath: "/owned/local-repository" });
    expect(mock.request.mock.calls.some((call) => call[1] === "create")).toBe(false);
  });
  it("allows project creation when no local model runner is installed", async () => {
    mock.request.mockImplementation(async (_scope, method) => method === "folderProjectAdd" ? null : method === "listPage" ? [source] : [{ id: "codex", available: false }]);
    render(page());
    await screen.findByRole("link", { name: /Original/ });
    expect(screen.getByRole("button", { name: "新建项目" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "新建项目" }));
    await waitFor(() => expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "folderProjectAdd", {}));
  });
  it.each([false, true])("keeps the current search and conversation when folder selection fails=%s or is cancelled", async (failed) => {
    mock.request.mockImplementation(async (_scope, method) => {
      if (method === "folderProjectAdd") { if (failed) throw new Error("Could not open folder picker"); return null; }
      return method === "listPage" ? [source] : [{ id: "codex", available: true }];
    });
    render(page());
    await screen.findByRole("link", { name: /Original/ });
    fireEvent.change(screen.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Original" } });
    fireEvent.click(screen.getByRole("button", { name: "新建项目" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "新建项目" })).toBeEnabled());
    expect(screen.getByRole("textbox", { name: "搜索会话" })).toHaveValue("Original");
    expect(mock.replace).not.toHaveBeenCalled();
    if (failed) expect(mock.error).toHaveBeenCalledWith("Could not open folder picker");
    else expect(mock.error).not.toHaveBeenCalled();
  });
});

describe("local conversation context menu", () => {
  afterEach(() => { window.getSelection()?.removeAllRanges(); });
  it.each(["context", "dropdown"])("copies the targeted Multica conversation ID from the %s menu", async (menu) => {
    const other = { ...source, id: "copied-multica-id", providerSessionId: "private-native-id", title: "Other conversation" };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, other] : []);
    mock.copyText.mockResolvedValue(true);
    render(page());
    const row = await screen.findByRole("link", { name: /Other conversation/ });
    if (menu === "context") fireEvent.contextMenu(row, { button: 2 });
    else await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Other conversation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "复制会话 ID" }));
    await waitFor(() => expect(mock.success).toHaveBeenCalledWith("已复制会话 ID"));
    expect(mock.copyText).toHaveBeenCalledExactlyOnceWith("copied-multica-id");
    expect(mock.request.mock.calls.every((call) => call[1] === "sideWorkspaceSync" || call[1] === "listPage" || call[1] === "read" || call[1] === "providers")).toBe(true);
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("reports clipboard failure without reporting success", async () => {
    mock.copyText.mockResolvedValue(false);
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Original/ }), { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "复制会话 ID" }));
    await waitFor(() => expect(mock.error).toHaveBeenCalledWith("无法复制会话 ID，请重试"));
    expect(mock.success).not.toHaveBeenCalled();
  });
  it.each(["context", "dropdown"])("renames the targeted session from the %s menu without leaving the active draft", async (menu) => {
    let other = { ...source, id: "other", title: "Other conversation" };
    mock.request.mockImplementation(async (_scope, method, params) => {
      if (method === "update") { other = { ...other, title: params.title }; return other; }
      return method === "listPage" ? [source, other] : [];
    });
    render(page());
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });
    const row = await screen.findByRole("link", { name: /Other conversation/ });
    if (menu === "context") fireEvent.contextMenu(row, { button: 2 });
    else await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Other conversation" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "重命名" }));
    const dialog = await screen.findByRole("dialog", { name: "重命名会话" });
    const input = within(dialog).getByRole("textbox", { name: "会话名称" });
    expect(input).toHaveValue("Other conversation");
    fireEvent.change(input, { target: { value: "   " } });
    expect(within(dialog).getByRole("button", { name: "保存" })).toBeDisabled();
    fireEvent.change(input, { target: { value: "  Renamed conversation  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "update", { id: "other", title: "Renamed conversation" });
    expect(screen.getByRole("link", { name: /Renamed conversation/ })).toBeInTheDocument();
    expect(mock.replace).not.toHaveBeenCalled();
    expect(draft).toHaveValue("Keep my draft");
  });
  it("keeps a failed rename editable and only closes after a successful retry", async () => {
    let reject!: (error: Error) => void;
    mock.request.mockImplementation(async (_scope, method) => method === "update" ? new Promise((_resolve, fail) => { reject = fail; }) : method === "listPage" ? [source] : []);
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Original/ }), { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "重命名" }));
    const dialog = await screen.findByRole("dialog", { name: "重命名会话" });
    const input = within(dialog).getByRole("textbox", { name: "会话名称" });
    fireEvent.change(input, { target: { value: "My title" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存" }));
    expect(within(dialog).getByRole("button", { name: "正在保存..." })).toBeDisabled();
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(dialog).toBeInTheDocument();
    await act(async () => { reject(new Error("Could not save title")); });
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Could not save title");
    expect(input).toHaveValue("My title");
    expect(input).toBeEnabled();
    mock.request.mockImplementation(async (_scope, method, params) => {
      if (method === "update") { source = { ...source, title: params.title }; return source; }
      return method === "listPage" ? [source] : [];
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "保存" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByRole("link", { name: /My title/ })).toBeInTheDocument();
  });
  it.each(["codex", "claude"] as const)("forks the right-clicked %s session and opens the saved branch", async (provider) => {
    const other = { ...source, id: "other", provider, title: "Fork this conversation" };
    const branch = { ...other, id: "branch", title: "Fork this conversation · Fork", forkedFrom: other.id };
    let rows = [source, other];
    let release!: (entry: LocalConversation) => void;
    mock.request.mockImplementation(async (_scope, method) => method === "fork" ? new Promise((resolve) => { release = resolve; }) : method === "listPage" ? rows : []);
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Fork this conversation/ }), { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "分叉" }));
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "fork", { id: "other" });
    expect(mock.replace).not.toHaveBeenCalled();
    rows = [...rows, branch];
    await act(async () => { release(branch); });
    await waitFor(() => expect(mock.replace).toHaveBeenCalledWith(`/test/conversations?provider=${provider}&session=branch`));
    expect(screen.getByRole("link", { name: /Fork this conversation · Fork/ })).toBeInTheDocument();
    expect(rows.find((entry) => entry.id === "source")).toBe(source);
  });
  it("disables full forks for a running or unbound session", async () => {
    source.turns = [{ id: "running", status: "running", events: [], createdAt: "2026-01-01" }];
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Original/ }), { button: 2 });
    expect(await screen.findByRole("menuitem", { name: "分叉" })).toHaveAttribute("aria-disabled", "true");
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    await act(async () => { qc.setQueryData(["test-local-sessions"], [{ ...source, providerSessionId: undefined, turns: [] }]); });
    fireEvent.contextMenu(screen.getByRole("link", { name: /Original/ }), { button: 2 });
    expect(await screen.findByRole("menuitem", { name: "分叉" })).toHaveAttribute("aria-disabled", "true");
    expect(mock.request.mock.calls.some((call) => call[1] === "fork")).toBe(false);
  });
  it("persists pin and unpin through the context menu", async () => {
    mock.request.mockImplementation(async (_scope, method, params) => {
      if (method === "update") { source = { ...source, pinned: params.pinned }; return source; }
      return method === "listPage" ? [source] : [];
    });
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Original/ }), { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "置顶" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "会话操作：Original" })).toBeEnabled());
    fireEvent.contextMenu(screen.getByRole("link", { name: /Original/ }), { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "取消置顶" }));
    await waitFor(() => expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "update", { id: "source", pinned: false }));
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("takes over right-click on an inactive row without navigating or replacing the draft", async () => {
    const other = { ...source, id: "other", title: "Other conversation" };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, other] : []);
    render(page());
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });
    const row = await screen.findByRole("link", { name: /Other conversation/ });
    expect(fireEvent.contextMenu(row, { button: 2, clientX: 120, clientY: 160 })).toBe(false);
    const menu = await screen.findByRole("menu", { name: "会话操作：Other conversation" });
    expect(menu).toHaveAttribute("data-slot", "context-menu-content");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "置顶" }));
    await waitFor(() => expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "update", { id: "other", pinned: true }));
    expect(mock.replace).not.toHaveBeenCalled();
    expect(draft).toHaveValue("Keep my draft");
  });
  it("moves the right-clicked session through the shared project submenu", async () => {
    mock.projects = [{ id: "project-a", workspace_id: "workspace", title: "Multica Project" }] as Project[];
    render(page());
    fireEvent.contextMenu(await screen.findByRole("link", { name: /Original/ }), { button: 2 });
    fireEvent.keyDown(await screen.findByRole("menuitem", { name: "关联工作区项目" }), { key: "ArrowRight" });
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Multica Project" }));
    await waitFor(() => expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "update", { id: "source", projectId: "project-a" }));
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("copies the selected row text and dismisses with Escape", async () => {
    mock.copyText.mockResolvedValue(true);
    render(page());
    const row = await screen.findByRole("link", { name: /Original/ });
    const selection = window.getSelection()!;
    const range = document.createRange();
    range.selectNodeContents(within(row).getByText("Original"));
    selection.removeAllRanges(); selection.addRange(range);
    fireEvent.contextMenu(row, { button: 2 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "复制所选文字" }));
    await waitFor(() => expect(mock.copyText).toHaveBeenCalledWith("Original"));
    selection.removeAllRanges();
    fireEvent.contextMenu(row, { button: 2 });
    const menu = await screen.findByRole("menu", { name: "会话操作：Original" });
    fireEvent.keyDown(menu, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());
    expect(mock.replace).not.toHaveBeenCalled();
  });
});

describe("Side conversations in the main session list", () => {
  it("preserves success, info, warning and error feedback in both main and Side panels", async () => {
    const side: LocalConversation = { ...source, id: "side", title: "Side exploration", side: { sourceId: source.id, turnId: "turn", quote: "Quoted source text", requestId: "side-request", context: [] } };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, side] : [{ id: "codex", available: true }]);
    localStorage.setItem("multica-side-workspace:user:workspace:session:source", JSON.stringify({ tabs: [{ id: "side-tab", kind: "side", sessionId: side.id }], selected: "side-tab", open: true }));
    render(page());
    for (const sessionId of [source.id, side.id]) {
      await waitFor(() => expect(mock.panel.mock.calls.some(([props]) => props.activeSessionId === sessionId)).toBe(true));
      const props = mock.panel.mock.calls.findLast(([props]) => props.activeSessionId === sessionId)![0] as ComponentProps<typeof TuttiAgentGuiPanel>;
      for (const tone of ["success", "info", "warning", "error"] as const) {
        const message = `${sessionId}:${tone}`;
        props.onShowMessage(message, tone);
        expect(mock[tone]).toHaveBeenLastCalledWith(message);
      }
      props.onShowMessage("Default feedback");
      expect(mock.info).toHaveBeenLastCalledWith("Default feedback");
    }
  });

  it.each(["codex", "claude"] as const)("keeps %s Sides in the right workspace while excluding them from the list, search, archives and counts", async (provider) => {
    mock.folders = [folder("project-a", "Main project")];
    const side: LocalConversation = { ...source, id: "side", title: "Side exploration", provider, side: { sourceId: source.id, turnId: "turn", quote: "Quoted source text", requestId: "side-request", context: [] } };
    const fork = { ...source, id: "fork", title: "Independent fork", cwd: "/other", forkedFrom: source.id };
    const archived = { ...source, id: "archived", title: "Archived main", archived: true };
    const rows = [source, fork, archived, side, { ...side, id: "side-draft", title: "Side draft", messages: [] }, { ...side, id: "side-archive", title: "Side archive", archived: true }];
    mock.request.mockImplementation(async (_scope, method, params) => method === "listPage" ? rows : method === "read" ? rows.find((entry) => entry.id === params.id) : [{ id: provider, available: true }]);
    localStorage.setItem("multica-side-workspace:user:workspace:session:source", JSON.stringify({ tabs: [{ id: "side-tab", kind: "side", sessionId: side.id }], selected: "side-tab", open: true }));
    render(page());
    const list = within(screen.getByTestId("local-conversation-sessions"));
    expect(await list.findByRole("link", { name: /Original/ })).toBeVisible();
    expect(list.getByRole("link", { name: /Independent fork/ })).toBeVisible();
    expect(list.queryAllByRole("link", { name: /Side/ })).toHaveLength(0);
    expect(within(list.getByRole("button", { name: "Main project" })).getByText("1")).toBeInTheDocument();
    expect(within(list.getByRole("button", { name: "未分配项目" })).getByText("1")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /旁支 · Side exploration ·/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Quoted source text")).toBeVisible();
    fireEvent.change(list.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Side" } });
    expect(await list.findByText("没有匹配的会话")).toBeVisible();
    fireEvent.change(list.getByRole("textbox", { name: "搜索会话" }), { target: { value: "" } });
    fireEvent.click(list.getByRole("button", { name: "归档" }));
    expect(await list.findByRole("link", { name: /Archived main/ })).toBeVisible();
    expect(list.queryAllByRole("link", { name: /Side/ })).toHaveLength(0);
    expect(listedIds()).not.toContain("side");
    fireEvent.click(screen.getByRole("button", { name: "定位来源" }));
    expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex&session=source");
    expect(await list.findByRole("link", { name: /Original/ })).toBeVisible();
    expect(list.queryAllByRole("link", { name: /Side/ })).toHaveLength(0);
  });
});

describe("local conversation directory label", () => {
  it.each([
    ["zh-Hans", "/profile/local-conversations/workdirs/3/source"],
    ["en", "C:\\profile\\local-conversations\\workdirs\\3\\source"],
    ["zh-Hans", `/profile/local-conversations/${"a".repeat(64)}/workdirs/source`],
    ["en", `/profile/local-conversations/${"a".repeat(64)}/workdirs/source`],
  ])("shows an unassigned project for the internal directory in %s: %s", async (locale, path) => {
    mock.locale = locale; source.cwd = path;
    render(page());
    const header = await screen.findByRole("button", { name: locale === "en" ? "Choose working directory" : "选择工作目录" });
    const label = locale === "en" ? "No project" : "未分配项目";
    expect(header).toHaveTextContent(label);
    expect(header).toHaveAttribute("title", expect.stringContaining(label));
    expect(header.getAttribute("title")).not.toContain(path);
  });

  it("keeps the name of a directory selected by the user", async () => {
    render(page());
    expect(await screen.findByRole("button", { name: "选择工作目录" })).toHaveTextContent("work");
  });
});

describe("local conversation fork entry", () => {
  it.each([
    ["zh-Hans", false, false], ["zh-Hans", true, true],
    ["en", false, true], ["en", true, false],
  ] as const)("omits header fork actions in %s (compact=%s, fork=%s)", async (locale, compact, forked) => {
    mock.locale = locale; mock.compact = compact;
    if (forked) {
      source.forkedFrom = "parent"; source.forkedFromTurnId = "parent-turn"; source.forkMode = "saved-context";
    }
    const parent = { ...source, id: "parent", title: "Parent", archived: true, forkedFrom: undefined };
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, parent] : [{ id: "codex", available: true }]);
    render(page());
    await screen.findByRole("button", { name: locale === "en" ? "Choose working directory" : "选择工作目录" });
    expect(screen.queryByRole("button", { name: /来源会话|Source conversation|分叉会话|Fork conversation/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: locale === "en" ? "Open right workspace" : "打开右侧面板" })).toBeVisible();
    if (compact) expect(screen.getByRole("button", { name: locale === "en" ? "Local sessions" : "本机会话" })).toBeVisible();
  });
  it.each(["codex", "claude"] as const)("keeps unsent %s drafts out of the list and counts until the first message is accepted", async (provider) => {
    const draft = { ...source, id: "draft", title: "Unsent draft", provider, providerSessionId: undefined, messages: [], turns: [] };
    let rows: LocalConversation[] = [source, { ...draft, id: "old-draft", title: "Older unsent draft" }];
    mock.request.mockImplementation(async (_scope, method) => {
      return method === "listPage" ? rows : [{ id: provider, available: true }];
    });
    const view = render(page());
    await screen.findByRole("link", { name: /Original/ });
    await waitFor(() => expect(screen.getByRole("button", { name: "新建会话" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    await waitFor(() => expect(mock.replace).toHaveBeenCalledWith(`/test/conversations?provider=${provider}`));
    expect(mock.request.mock.calls.some((call) => call[1] === "create")).toBe(false);
    expect(listedIds()).toEqual(["source"]);
    mock.selected = "";
    view.rerender(page());
    const firstRequest = JSON.parse(screen.getByRole("textbox", { name: "Conversation draft" }).getAttribute("data-new-conversation")!);
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    expect(JSON.parse(screen.getByRole("textbox", { name: "Conversation draft" }).getAttribute("data-new-conversation")!).sequence).toBeGreaterThan(firstRequest.sequence);
    expect(mock.request.mock.calls.some((call) => call[1] === "create")).toBe(false);
    fireEvent.change(screen.getByRole("textbox", { name: "Conversation draft" }), { target: { value: "Not sent yet" } });
    expect(screen.queryAllByRole("link", { name: /Unsent draft|Older unsent draft/ })).toHaveLength(0);
    expect(within(screen.getByRole("button", { name: "未分配项目" })).getByText("1")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toHaveValue("Not sent yet");

    const at = "2026-10-24T18:00:00Z";
    rows = [...rows, { ...draft, title: "First submitted message", messages: [{ id: "first-message", role: "user", content: "First submitted message", turnId: "first-turn", createdAt: at }], turns: [{ id: "first-turn", status: "running", createdAt: at, events: [] }] }];
    await act(async () => { await publishList(); });
    expect(await screen.findByRole("link", { name: /First submitted message/ })).toBeInTheDocument();
    expect(within(screen.getByRole("button", { name: "未分配项目" })).getByText("2")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Older unsent draft/ })).not.toBeInTheDocument();
  });
  it("collapses folder groups, reveals search matches, and opens a draft inside the folder", async () => {
    mock.folders = [folder("project-a", "Multica Project")];
    const rows = [source];
    mock.request.mockImplementation(async (_scope, method) => {
      return method === "listPage" ? rows : [{ id: "codex", available: true }];
    });
    render(page());
    const group = await screen.findByRole("button", { name: "Multica Project" });
    fireEvent.click(group);
    expect(group).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("link", { name: /Original/ })).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "搜索会话" }), { target: { value: "Multica Project" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Multica Project" })).toHaveAttribute("aria-expanded", "true"));
    expect(screen.getByRole("link", { name: /Original/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "新建项目会话：Multica Project" }));
    expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex");
    expect(JSON.parse(screen.getByRole("textbox", { name: "Conversation draft" }).getAttribute("data-new-conversation")!)).toMatchObject({ projectPath: "/owned/work" });
    expect(mock.request.mock.calls.some((call) => call[1] === "create")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Multica Project" }));
    expect(screen.getAllByRole("link", { name: /Original/ })).toHaveLength(1);
  });
  it("keeps failed moves in their original group and exposes recovery", async () => {
    mock.projects = [{ id: "project-a", workspace_id: "workspace", title: "Multica Project" }] as Project[];
    mock.request.mockImplementation(async (_scope, method) => {
      if (method === "update") throw new Error("Could not save project");
      return method === "listPage" ? [source] : [];
    });
    render(page());
    await clickMenuTrigger(await screen.findByRole("button", { name: "会话操作：Original" }));
    fireEvent.keyDown(await screen.findByRole("menuitem", { name: "关联工作区项目" }), { key: "ArrowRight" });
    fireEvent.click(await screen.findByRole("menuitemradio", { name: "Multica Project" }));
    await waitFor(() => expect(mock.error).toHaveBeenCalledWith("Could not save project"));
    expect(screen.getByRole("region", { name: "未分配项目" })).toContainElement(screen.getByRole("link", { name: /Original/ }));
    expect(screen.getByRole("button", { name: "会话操作：Original" })).not.toBeDisabled();
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("links a cloud project without changing local folder grouping or replacing its draft", async () => {
    mock.folders = [folder("local-folder", "Local folder")];
    mock.projects = [{ id: "project-a", workspace_id: "workspace", title: "Multica Project" }] as Project[];
    mock.request.mockImplementation(async (_scope, method, params) => {
      if (method === "update") { source = { ...source, projectId: params.projectId }; return source; }
      return method === "listPage" ? [source] : [{ id: "codex", available: true }];
    });
    render(page());
    const draft = screen.getByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Keep my draft" } });
    const move = async (label: string) => {
      await clickMenuTrigger(await screen.findByRole("button", { name: "会话操作：Original" }));
      fireEvent.keyDown(await screen.findByRole("menuitem", { name: "关联工作区项目" }), { key: "ArrowRight" });
      fireEvent.click(await screen.findByRole("menuitemradio", { name: label }));
    };
    await move("Multica Project");
    await waitFor(() => expect(screen.getByRole("region", { name: "Local folder" })).toContainElement(screen.getByRole("link", { name: /Original/ })));
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "update", { id: "source", projectId: "project-a" });
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(draft);
    expect(draft).toHaveValue("Keep my draft");
    await move("不关联工作区项目");
    await waitFor(() => expect(screen.getByRole("region", { name: "Local folder" })).toContainElement(screen.getByRole("link", { name: /Original/ })));
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("resizes with the pointer, respects limits, and restores the saved width", async () => {
    vi.stubGlobal("PointerEvent", MouseEvent);
    let groupWidth = 1000;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      const list = document.getElementById("local-sessions");
      const width = list?.style.flexGrow ? Number(list.style.flexGrow) * groupWidth / 100 : 268;
      if (this.getAttribute("data-slot") === "resizable-handle") return new DOMRect(width, 0, 0, 700);
      if (this.id === "local-sessions") return new DOMRect(0, 0, width, 700);
      if (this.id === "local-detail") return new DOMRect(width, 0, groupWidth - width, 700);
      return new DOMRect(0, 0, groupWidth, 700);
    });
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(function (this: HTMLElement) { return this.getBoundingClientRect().width; });
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) { return this.getBoundingClientRect().height; });
    vi.spyOn(HTMLElement.prototype, "offsetLeft", "get").mockImplementation(function (this: HTMLElement) { return this.getBoundingClientRect().left; });
    let view = render(page());
    const handle = await screen.findByRole("separator", { name: "调整会话列表宽度" });
    await waitFor(() => expect(handle).toHaveAttribute("aria-valuenow", "26.8"));
    const drag = (from: number, to: number) => {
      fireEvent.pointerDown(handle, { clientX: from, clientY: 300, button: 0, buttons: 1 });
      fireEvent.pointerMove(document, { clientX: to, clientY: 300, buttons: 1 });
      fireEvent.pointerUp(document, { clientX: to, clientY: 300, buttons: 0 });
    };
    drag(268, 410);
    expect(handle).toHaveAttribute("aria-valuenow", "41");
    drag(410, 900);
    expect(handle).toHaveAttribute("aria-valuenow", "48");
    drag(480, 100);
    expect(handle).toHaveAttribute("aria-valuenow", "24");
    drag(240, 350);
    expect(handle).toHaveAttribute("aria-valuenow", "35");
    view.unmount();
    groupWidth = 1200;
    view = render(page());
    await waitFor(() => expect(document.getElementById("local-sessions")!.getBoundingClientRect().width).toBeCloseTo(350));
    view.unmount();
    groupWidth = 500;
    view = render(page());
    await waitFor(() => expect(document.getElementById("local-sessions")!.getBoundingClientRect().width).toBeLessThanOrEqual(300));
    view.unmount();
    groupWidth = 1000;
    view = render(page());
    await waitFor(() => expect(document.getElementById("local-sessions")!.getBoundingClientRect().width).toBeCloseTo(350));
    const restoredHandle = screen.getByRole("separator", { name: "调整会话列表宽度" });
    fireEvent.keyDown(restoredHandle, { key: "ArrowRight" });
    fireEvent.keyUp(restoredHandle, { key: "ArrowRight" });
    const keyboardWidth = document.getElementById("local-sessions")!.getBoundingClientRect().width;
    expect(keyboardWidth).toBeGreaterThan(350);
    view.unmount();
    view = render(page());
    await waitFor(() => expect(document.getElementById("local-sessions")!.getBoundingClientRect().width).toBeCloseTo(keyboardWidth));
    view.unmount();
  });
  it("keeps the mounted conversation and draft while switching between resizable and compact layouts", async () => {
    const view = render(page());
    const draft = await screen.findByRole("textbox", { name: "Conversation draft" });
    fireEvent.change(draft, { target: { value: "Unsent draft" } });
    expect(screen.getByRole("separator", { name: "调整会话列表宽度" })).toBeInTheDocument();
    mock.compact = true; view.rerender(page());
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(draft);
    expect(draft).toHaveValue("Unsent draft");
    expect(screen.queryByRole("separator", { name: "调整会话列表宽度" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "本机会话" }));
    expect(draft).toBeInTheDocument();
    mock.compact = false; view.rerender(page());
    expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(draft);
    expect(draft).toHaveValue("Unsent draft");
  });
  it("shows pending state and opens the persisted branch after a successful fork", async () => {
    let release!: (branch: LocalConversation) => void;
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source] : method === "fork" ? new Promise((resolve) => { release = resolve; }) : []);
    render(page());
    await clickMenuTrigger(await screen.findByRole("button", { name: "会话操作：Original" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "分叉" }));
    await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Original" }));
    const button = await screen.findByRole("menuitem", { name: "正在分叉…" });
    expect(mock.request).toHaveBeenCalledWith({ userId: "user", workspaceId: "workspace" }, "fork", { id: "source" });
    expect(button).toHaveAttribute("aria-disabled", "true"); expect(button).toHaveAttribute("aria-busy", "true");
    expect(mock.replace).not.toHaveBeenCalled();
    release({ ...source, id: "branch", forkedFrom: "source" });
    await waitFor(() => expect(mock.replace).toHaveBeenCalledWith("/test/conversations?provider=codex&session=branch"));
    expect(await screen.findByRole("menuitem", { name: "分叉" })).toHaveAttribute("aria-busy", "false");
  });
  it("keeps the current conversation on failure and does not redirect after the user selects another session", async () => {
    mock.request.mockImplementation(async (_scope, method) => { if (method === "fork") throw new Error("Native fork failed"); return method === "listPage" ? [source] : []; });
    const view = render(page());
    await clickMenuTrigger(await screen.findByRole("button", { name: "会话操作：Original" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "分叉" }));
    await waitFor(() => expect(mock.error).toHaveBeenCalledWith("Native fork failed"));
    expect(mock.replace).not.toHaveBeenCalled();
    let release!: (branch: LocalConversation) => void;
    mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, { ...source, id: "other" }] : method === "fork" ? new Promise((resolve) => { release = resolve; }) : []);
    await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Original" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "分叉" }));
    await clickMenuTrigger(screen.getByRole("button", { name: "会话操作：Original" }));
    act(() => { qc.setQueryData(["test-local-sessions"], [source, { ...source, id: "other" }]); });
    mock.selected = "other"; view.rerender(page());
    release({ ...source, id: "branch" });
    await waitFor(() => expect(screen.getByRole("menuitem", { name: "分叉" })).toHaveAttribute("aria-busy", "false"));
    expect(mock.replace).not.toHaveBeenCalled();
  });
  it("disables forking while an execution is running", async () => {
    source.turns = [{ id: "turn", status: "running", createdAt: "2026-01-01", events: [] }];
    render(page());
    await clickMenuTrigger(await screen.findByRole("button", { name: "会话操作：Original" }));
    expect(await screen.findByRole("menuitem", { name: "分叉" })).toHaveAttribute("aria-disabled", "true");
  });
});

it("restores each route's Side toggle without remounting the main editor", async () => {
  const b = { ...source, id: "second", title: "Second" };
  mock.request.mockImplementation(async (_scope, method) => method === "listPage" ? [source, b] : []);
  const view = render(page());
  await screen.findByRole("link", { name: /Original/ });
  const main = screen.getByRole("textbox", { name: "Conversation draft" });
  fireEvent.change(main, { target: { value: "Keep draft" } });
  fireEvent.click(screen.getByRole("button", { name: "打开右侧面板" }));
  expect(screen.getByText("在这里继续旁支对话，或查看文件和网页")).toBeVisible();
  mock.selected = b.id; view.rerender(page());
  expect(screen.getByRole("button", { name: "打开右侧面板" })).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText("在这里继续旁支对话，或查看文件和网页")).not.toBeVisible();
  mock.selected = source.id; view.rerender(page());
  expect(screen.getByText("在这里继续旁支对话，或查看文件和网页")).toBeVisible();
  expect(screen.getByRole("textbox", { name: "Conversation draft" })).toBe(main);
  expect(main).toHaveValue("Keep draft");
});

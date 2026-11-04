import { createRef } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { LocalConversation, LocalConversationScope } from "@multica/core/local-conversation/protocol";
import { LocalSideWorkspace, type SideWorkspaceHandle } from "./local-side-workspace";
import { sessionEntry } from "../platform/local-conversation-projects";
import chat from "../../../../../../packages/views/locales/zh-Hans/chat.json";
vi.mock("../platform/local-v2-transport", () => ({ localV2Transport: () => window.desktopAPI.localV2 }));

vi.mock("@multica/views/i18n", () => ({ useT: () => ({ t: (selector: (resource: typeof chat) => string, options?: Record<string, string | number>) => selector(chat).replace(/{{(\w+)}}/g, (_, key) => String(options?.[key] ?? "")) }) }));
vi.mock("../components/tutti-agent-gui-panel", () => ({ TuttiAgentGuiPanel: ({ activeSessionId, active }: { activeSessionId: string; active: boolean }) => <textarea aria-label={`Side draft ${activeSessionId}`} data-active={String(active)} /> }));
const scope = { userId: "side-user", workspaceId: "side-workspace" };
let queryClient: QueryClient;
let sessions: LocalConversation[];
let request: ReturnType<typeof vi.fn<(scope: unknown, method: string, params: Record<string, unknown>) => Promise<unknown>>>;
const preview = { request: vi.fn(), subscribe: vi.fn(() => () => {}) };
const changes = new Set<(scope: LocalConversationScope, id: string) => void>();
const at = "2026-10-24T00:00:00Z";
const time = (value: string) => value.includes("T") ? value : `${value}T00:00:00Z`;

/** The cases keep v1-shaped fixtures; this is the daemon's v2 view of them. */
function summary(entry: LocalConversation) {
  return { conversation_id: entry.id, summary_seq: 1, provider: entry.provider, title: entry.title, cwd: entry.cwd, project_id: null, pinned: entry.pinned, archived: entry.archived,
    status: entry.turns.some((turn) => turn.status === "running") ? "running" : "idle", last_message_preview: entry.messages.at(-1)?.content ?? "",
    turn_count: Math.max(entry.turns.length, entry.messages.length ? 1 : 0), updated_at: time(entry.updatedAt), created_at: time(entry.createdAt),
    ...(entry.side ? { side_source_id: entry.side.sourceId, ...(entry.side.quote ? { side_quote: entry.side.quote } : {}) } : {}) };
}
function snapshot(entry: LocalConversation) {
  let seq = 0;
  const item = (turnId: string, id: string, kind: string, status: string, state: Record<string, unknown>) => [
    { seq: ++seq, turn_id: turnId, item_id: id, type: "item.started", final: false, created_at: at, payload: { kind } },
    { seq: ++seq, turn_id: turnId, item_id: id, type: status === "in_progress" ? "item.updated" : "item.completed", final: status !== "in_progress", created_at: at, payload: { kind, status, ...state } },
  ];
  const turns = entry.turns.map((turn, index) => {
    const answer = entry.messages.filter((message) => message.role === "assistant" && message.turnId === turn.id).at(-1)?.content
      ?? turn.events.filter((event) => event.type === "text").map((event) => event.content ?? "").join("");
    const tools = turn.events.flatMap((event) => event.type === "tool_use" ? item(turn.id, `${turn.id}-${event.seq}`, "tool_call", "completed", { tool: event.tool, input: event.input }) : []);
    return { id: turn.id, turn_index: index, request_id: turn.id, status: turn.status, started_at: time(turn.createdAt), ...(turn.status === "running" ? {} : { completed_at: at }),
      ...(turn.error ? { error: { kind: "provider", message: turn.error } } : {}), messages: [],
      events: [...tools, ...(answer ? item(turn.id, `${turn.id}-text`, "assistant_text", turn.status === "running" ? "in_progress" : "completed", { text: answer }) : [])] };
  });
  return { version: 2, store_generation: "g", snapshot_seq: seq, conversation: summary(entry), turns };
}
const previewTexts = new Map<string, string>();
/** Daemon errors arrive with their kind; the fixtures name them like Node did. */
function daemonError(error: unknown): never {
  const message = error instanceof Error ? error.message : String(error);
  throw new Error(/ENOENT/.test(message) ? `not_found: ${message}` : /outside/.test(message) ? `outside_folder: ${message}` : message);
}
async function daemon(scopeValue: LocalConversationScope, method: string, path: string, body?: Record<string, unknown>): Promise<unknown> {
  const url = new URL(path, "http://daemon"), q = url.searchParams;
  const session = /^\/sessions\/([^/]+)\/(snapshot|commands)$/.exec(url.pathname);
  try {
    if (session?.[2] === "snapshot") {
      const entry = await request(scopeValue, "read", { id: session[1] }) as LocalConversation | undefined;
      if (!entry) throw new Error("not_found: Local conversation not found");
      return snapshot(entry);
    }
    if (session && body?.type === "create_side") return summary(await request(scopeValue, "sideCreate", { id: session[1], requestId: body.request_id }) as LocalConversation);
    if (session && body?.type === "side_workspace_sync") return request(scopeValue, "sideWorkspaceSync", body.workspace as Record<string, unknown>);
    if (session && body?.type === "interrupt") return request(scopeValue, "cancel", { id: session[1] });
    if (url.pathname === "/sessions" && method === "POST") return summary(await request(scopeValue, "create", { provider: body!.provider }) as LocalConversation);
    const location = { ...(q.get("root") ? { root: q.get("root")! } : {}), path: q.get("path")!, ...(q.get("conversation_id") ? { id: q.get("conversation_id")! } : {}) };
    if (url.pathname === "/preview/locate") return await request(scopeValue, "previewLocate", location);
    if (url.pathname === "/preview/list") return { entries: await request(scopeValue, "previewList", location) };
    if (url.pathname === "/preview/file") {
      if (!location.root) throw new Error("outside_folder: choose this folder before previewing its contents");
      const file = await request(scopeValue, "previewRead", location) as { root?: string; path: string; name: string; mimeType: string; size: number; text?: string };
      previewTexts.set(url.search, file.text ?? "");
      return { root: file.root ?? "/project", path: file.path, name: file.name, mime_type: file.mimeType, size: file.size };
    }
  } catch (error) { daemonError(error); }
  throw new Error(`http_404: ${method} ${path}`);
}
beforeEach(() => {
  localStorage.clear(); vi.clearAllMocks(); changes.clear();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  sessions = [{ id: "main", provider: "codex", title: "Main conversation", cwd: "/project", model: "main-model", messages: [], turns: [], createdAt: "2026-10-24", updatedAt: "2026-10-24", archived: false, pinned: false }];
  request = vi.fn(async (_scope, method, params) => {
    if (method === "sideCreate") { const side = { ...sessions[0]!, id: "new-side", side: { sourceId: "main", requestId: String(params.requestId), context: [] } }; sessions.push(side); return side; }
    if (method === "read") return sessions.find((entry) => entry.id === params.id);
    if (method === "previewChoose") return { root: "/project", path: "/project", name: "project", kind: "directory" };
    if (method === "previewLocate") {
      if (!String(params.path).startsWith("/project/")) throw new Error("outside_folder: choose this folder before previewing its contents");
      return { root: "/project", path: params.path, name: String(params.path).split("/").at(-1), kind: "file" };
    }
    if (method === "previewList") return [{ root: "/project", path: "/project/test.ts", name: "test.ts", kind: "file" }, { root: "/project", path: "/project/nested", name: "nested", kind: "directory" }];
    if (method === "previewRead") return { name: "test.ts", path: "/project/test.ts", mimeType: "text/plain", text: "export const ready = true;", size: 26, truncated: false };
    return undefined;
  });
  preview.request.mockImplementation(async (_scope, command) => command.action === "close" || command.action === "hide" ? null : { id: command.id, url: command.url, title: "Example", loading: false, canGoBack: false, canGoForward: false });
  previewTexts.clear();
  vi.stubGlobal("fetch", async (url: string) => new Response(previewTexts.get(new URL(url.replace(/^v2:/, "http://daemon")).search) ?? ""));
  const messages = new Set<(scope: LocalConversationScope, message: unknown) => void>();
  // A change notice from a case makes the conversation's subscription read it again.
  changes.add((changed, id) => messages.forEach((listener) => listener(changed, { version: 2, type: "subscription.status", conversation_id: id, status: "snapshot_required" })));
  const localV2 = { request: daemon, send: vi.fn(async () => {}), subscribe: (listener: (scope: LocalConversationScope, message: unknown) => void) => { messages.add(listener); return () => messages.delete(listener); },
    onConnection: () => () => {}, fileUrl: (_scope: unknown, path: string) => `v2:${path}`, upload: vi.fn(),
    availability: () => ({ state: "connected" }), onAvailability: () => () => {},
    choose: async (scopeValue: LocalConversationScope, choice: { kind: string }) => request(scopeValue, "previewChoose", { kind: choice.kind }) };
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { localV2, resourcePreview: preview, openExternal: vi.fn() } });
});
afterEach(() => { cleanup(); queryClient.clear(); vi.unstubAllGlobals(); localStorage.clear(); });
function setup() {
  const ref = createRef<SideWorkspaceHandle>(); const onSelectSource = vi.fn(), onShowMessage = vi.fn();
  const element = (source: LocalConversation | undefined, sessionId: string | null = source?.id ?? null) => <QueryClientProvider client={queryClient}><LocalSideWorkspace ref={ref} scope={scope} slug="test" source={source && sessionEntry(summary(source) as never)} sessionId={sessionId} provider="codex" sessions={sessions.map((entry) => sessionEntry(summary(entry) as never))} chinese active onSelectSource={onSelectSource} onOpenTask={() => {}} onShowMessage={onShowMessage} onReference={() => {}}>{(visible) => <textarea aria-label="Main draft" data-visible={String(visible)} />}</LocalSideWorkspace></QueryClientProvider>;
  const view = render(element(sessions[0]));
  return { ...view, ref, onSelectSource, onShowMessage, select: (source: LocalConversation | null) => view.rerender(element(source ?? undefined)), selectId: (id: string) => view.rerender(element(undefined, id)) };
}
it("creates a Side without replacing the main draft, retains its draft when closed, and returns to its source", async () => {
  const { ref, onSelectSource } = setup(); const main = screen.getByRole("textbox", { name: "Main draft" });
  fireEvent.change(main, { target: { value: "Keep main text" } });
  ref.current!.toggle();
  fireEvent.click(await screen.findByRole("button", { name: "新建旁支" }));
  const side = await screen.findByRole("textbox", { name: "Side draft new-side" });
  expect(request).toHaveBeenCalledWith(scope, "sideCreate", expect.objectContaining({ id: "main" }));
  expect(screen.getByRole("textbox", { name: "Main draft" })).toBe(main); expect(main).toHaveValue("Keep main text");
  fireEvent.change(side, { target: { value: "Keep side text" } });
  fireEvent.click(screen.getByRole("button", { name: "定位来源" })); expect(onSelectSource).toHaveBeenCalledWith("main", undefined, undefined);
  fireEvent.click(screen.getByRole("button", { name: "关闭右侧面板" }));
  await waitFor(() => expect(side).toHaveAttribute("data-active", "false"));
  ref.current!.toggle();
  expect(await screen.findByRole("textbox", { name: "Side draft new-side" })).toBe(side); expect(side).toHaveValue("Keep side text");
});
it("shows a running Side before its first output and retains the result after reopening", async () => {
  const side: LocalConversation = { ...sessions[0]!, id: "working-side", title: "Main conversation · Side", side: { sourceId: "main", requestId: "work", context: [] },
    turns: [{ id: "work", status: "running" as const, createdAt: "2026-10-24", events: [] }] };
  sessions.push(side);
  const view = setup();
  act(() => view.ref.current!.openSide(side.id));
  expect(await screen.findByRole("tab", { name: /旁支.*运行中/ })).toHaveAttribute("aria-busy", "true");
  expect(await screen.findByText("正在等待首次输出...")).toBeVisible();
  const base = request.getMockImplementation()!;
  const done: LocalConversation = { ...side, updatedAt: "2026-10-25", turns: [{ ...side.turns[0]!, status: "completed" }], messages: [{ id: "answer", turnId: "work", role: "assistant", content: "已检查标签，并验证两种主题。", createdAt: "2026-10-25" }] };
  request.mockImplementation((scope, method, params) => method === "read" && params.id === side.id ? Promise.resolve(done) : base(scope, method, params));
  sessions[sessions.indexOf(side)] = { ...done, turns: [] };
  await act(async () => { changes.forEach((listener) => listener(scope, side.id)); });
  expect(await screen.findByRole("tab", { name: /旁支.*已完成/ })).toHaveAttribute("aria-busy", "false");
  expect(screen.getByText("工作摘要")).toBeVisible();
  expect(screen.getAllByText("已检查标签，并验证两种主题。")[0]).toBeVisible();
  expect(screen.queryByText("正在等待首次输出...")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "关闭右侧面板" }));
  act(() => view.ref.current!.toggle());
  expect(await screen.findByText("工作摘要")).toBeVisible();
  view.unmount(); setup();
  expect(await screen.findByText("工作摘要")).toBeVisible();
});
it("keeps each Side's activity separate across tab and main conversation switches", async () => {
  const a: LocalConversation = { ...sessions[0]!, id: "side-a", title: "A", turns: [{ id: "a", status: "running", events: [], createdAt: "2026-10-24" }] };
  const b: LocalConversation = { ...sessions[0]!, id: "side-b", title: "B", turns: [{ id: "b", status: "cancelled", events: [{ seq: 1, type: "text", content: "B 的部分输出", created_at: "2026-10-24" }], createdAt: "2026-10-24" }] };
  const other = { ...sessions[0]!, id: "other-main" };
  sessions.push(a, b, other);
  const view = setup();
  act(() => { view.ref.current!.openSide(a.id); view.ref.current!.openSide(b.id); });
  expect(await screen.findByRole("tab", { name: "旁支 · A · 运行中" })).toHaveAttribute("aria-busy", "true");
  expect(await screen.findByRole("tab", { name: "旁支 · B · 已取消" })).toHaveAttribute("aria-busy", "false");
  expect(screen.getAllByText("B 的部分输出")[0]).toBeVisible();
  fireEvent.click(screen.getByRole("tab", { name: "旁支 · A · 运行中" }));
  expect(screen.getByText("正在等待首次输出...")).toBeVisible();
  expect(screen.getAllByText("B 的部分输出")[0]).not.toBeVisible();
  const reads = request.mock.calls.filter(([, method]) => method === "read").length;
  await act(async () => { changes.forEach((listener) => listener({ ...scope, workspaceId: "another-workspace" }, a.id)); });
  expect(request.mock.calls.filter(([, method]) => method === "read")).toHaveLength(reads);
  view.select(other);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: "旁支 · A · 运行中" })).toHaveAttribute("aria-selected", "true");
});
it("shows a retry action when the activity detail cannot be read", async () => {
  const side: LocalConversation = { ...sessions[0]!, id: "retry-side", title: "Retry", turns: [{ id: "failed", status: "failed", error: "Connection interrupted", createdAt: "2026-10-24", events: [] }] };
  sessions.push({ ...side, turns: [] });
  let unavailable = true;
  const base = request.getMockImplementation()!;
  request.mockImplementation(async (scope, method, params) => {
    if (method === "read" && params.id === side.id) {
      if (unavailable) throw new Error("Read failed");
      return side;
    }
    return base(scope, method, params);
  });
  const view = setup(); act(() => view.ref.current!.openSide(side.id));
  expect(await screen.findByText("无法加载工作摘要。")).toBeVisible();
  expect(screen.queryByText("工作摘要")).not.toBeInTheDocument();
  unavailable = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByRole("tab", { name: "旁支 · Retry · 执行失败" })).toHaveAttribute("aria-busy", "false");
  expect(screen.getByText("Connection interrupted")).toBeVisible();
  expect(screen.queryByText("无法加载工作摘要。")).not.toBeInTheDocument();
});
it("uses the shared tabs for a folder preview, loads subfolders lazily and reads the selected file", async () => {
  const { ref } = setup(); ref.current!.toggle();
  fireEvent.click(await screen.findByRole("button", { name: "添加右侧内容" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "打开文件夹" }));
  const tab = await screen.findByRole("tab", { name: "project" }); expect(tab).toHaveAttribute("aria-selected", "true");
  const file = await screen.findByRole("button", { name: "test.ts" });
  expect(request.mock.calls.filter((call) => call[1] === "previewList")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "nested" })).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(file);
  expect(await screen.findByText("export const ready = true;")).toBeVisible();
  expect(request).toHaveBeenCalledWith(scope, "previewRead", expect.objectContaining({ path: "/project/test.ts" }));
});
it.each([
  ["file:///project/output/%E6%88%BF%E9%97%B4%20report.html#L12", "/project/output/房间 report.html", 12],
  ["/project/output/%E6%88%BF%E9%97%B4%20report.html:12", "/project/output/房间 report.html", 12],
  ["output/cases.html", "/project/output/cases.html", undefined],
])("previews a transcript file using the source folder and decoded path: %s", async (href, path, line) => {
  const { ref } = setup();
  await act(async () => { await ref.current!.openResource("main", href); });
  expect(request).toHaveBeenCalledWith(scope, "previewLocate", { path, id: "main" });
  expect(request).toHaveBeenCalledWith(scope, "previewRead", { root: "/project", path, id: "main" });
  expect(await screen.findByRole("tab", { name: "test.ts" })).toHaveAttribute("aria-selected", "true");
  if (line) expect(await screen.findByText(`第 ${line} 行`)).toBeVisible();
});
it("rejects foreign file URLs before reading a local resource", async () => {
  const { ref } = setup();
  await expect(ref.current!.openResource("main", "file://server/project/cases.html")).rejects.toThrow("Use a local file path");
  expect(request.mock.calls.filter(([, method]) => method === "previewRead")).toEqual([]);
});
it("previews an HTML link with its authorized root without asking for another file selection", async () => {
  const base = request.getMockImplementation()!;
  const html = "<!doctype html><html><body>Login</body></html>";
  request.mockImplementation(async (scopeValue, method, params) => method === "previewRead"
    ? { root: "/project", path: "/project/login.html", name: "login.html", mimeType: "text/html", text: html, size: html.length, truncated: false }
    : base(scopeValue, method, params));
  const view = setup();
  await act(async () => { await view.ref.current!.openResource("main", "/project/login.html"); });
  expect(await screen.findByRole("tab", { name: "login.html" })).toHaveAttribute("aria-selected", "true");
  expect(view.container.querySelector("iframe")?.getAttribute("srcdoc")).toContain(html);
  expect(request).toHaveBeenCalledWith(scope, "previewRead", { root: "/project", path: "/project/login.html", id: "main" });
  expect(request.mock.calls.some(([, method]) => method === "previewChoose")).toBe(false);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
it("keeps files outside the authorized folders blocked before reading their contents", async () => {
  const { ref } = setup();
  await act(async () => { await ref.current!.openResource("main", "/private/secret.html"); });
  expect(screen.getByRole("alert")).toHaveTextContent("无法访问此文件");
  expect(request.mock.calls.some(([, method]) => method === "previewRead" || method === "previewChoose")).toBe(false);
});
it("keeps the authorized root when a tool reference resolves outside the source cwd", async () => {
  const path = "/selected repo/server/internal/daemon/runtime_mcp.go";
  sessions[0]!.turns = [{ id: "turn", status: "completed", createdAt: "2026-10-24", events: [
    { type: "tool_use", seq: 1, tool: "Read", input: { file_path: path }, created_at: "2026-10-24" },
  ] }];
  request.mockImplementation(async (_scope, method, params) => {
    if (method === "read") return sessions[0];
    if (method === "previewLocate") {
      if (params.path !== path) throw new Error("ENOENT");
      return { root: "/selected repo", path, name: "runtime_mcp.go", kind: "file" };
    }
    if (method === "previewRead") {
      if (params.path !== path) throw new Error("ENOENT");
      return { root: "/selected repo", path, name: "runtime_mcp.go", mimeType: "text/plain", text: "package daemon", size: 14, truncated: false };
    }
    return undefined;
  });
  const { ref } = setup();
  await act(async () => { await ref.current!.openResource("main", "runtime_mcp.go"); });
  expect(await screen.findByText("package daemon")).toBeVisible();
  expect(request).toHaveBeenCalledWith(scope, "previewRead", { root: "/selected repo", path, id: "main" });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
it("shows one recoverable file error and preserves the main draft instead of stacking IPC toasts", async () => {
  let missing = true;
  const base = request.getMockImplementation()!;
  request.mockImplementation(async (_scope, method, params) => {
    if (method === "read") return sessions[0];
    if (method === "previewRead") {
      if (missing) throw new Error("Error invoking remote method 'local-v2:request': Error: ENOENT: realpath '/project/walkthrough.md'");
      return { name: "test.ts", path: "/project/test.ts", mimeType: "text/plain", text: "ready", size: 5, truncated: false };
    }
    return base(_scope, method, params);
  });
  const { ref, onShowMessage } = setup(); const main = screen.getByRole("textbox", { name: "Main draft" });
  fireEvent.change(main, { target: { value: "Keep this draft" } });
  await act(async () => { await Promise.all([ref.current!.openResource("main", "walkthrough.md"), ref.current!.openResource("main", "walkthrough.md")]); });
  expect(request.mock.calls.filter(([, method]) => method === "previewRead")).toHaveLength(1);
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getByRole("alert")).toHaveTextContent("找不到此文件");
  expect(screen.getByRole("alert")).not.toHaveTextContent(/ENOENT|remote method|realpath/);
  expect(onShowMessage).not.toHaveBeenCalled(); expect(main).toHaveValue("Keep this draft");
  missing = false;
  await act(async () => { await ref.current!.openResource("main", "/project/test.ts"); });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Main draft" })).toBe(main);
});
it("opens a web tab in the same workspace and closes its native view with the tab", async () => {
  const { ref } = setup(); ref.current!.toggle();
  fireEvent.click(await screen.findByRole("button", { name: "添加右侧内容" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "打开网页" }));
  const dialog = await screen.findByRole("dialog", { name: "打开网页" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "网页地址" }), { target: { value: "https://example.com/guide" } });
  fireEvent.submit(within(dialog).getByRole("button", { name: "打开" }).closest("form")!);
  expect(await screen.findByRole("tab", { name: "example.com" })).toBeVisible();
  await waitFor(() => expect(preview.request.mock.calls.some(([, command]) => command.action === "show" && command.url === "https://example.com/guide")).toBe(true));
  const globalPreview = document.createElement("dialog");
  globalPreview.setAttribute("role", "presentation");
  document.body.append(globalPreview);
  await act(async () => { globalPreview.setAttribute("role", "dialog"); });
  await waitFor(() => expect(preview.request.mock.calls.at(-1)?.[1].action).toBe("hide"));
  await act(async () => { globalPreview.setAttribute("role", "presentation"); });
  await waitFor(() => expect(preview.request.mock.calls.at(-1)?.[1].action).toBe("show"));
  globalPreview.remove();
  fireEvent.click(screen.getByRole("button", { name: "关闭 example.com" }));
  await waitFor(() => expect(preview.request.mock.calls.some(([, command]) => command.action === "close")).toBe(true));
});
it("hides the empty Side hint once the list summary keeps only a message from a finished turn", async () => {
  sessions.push({ ...sessions[0]!, id: "answered-side", title: "Main conversation · Side", side: { sourceId: "main", requestId: "answered", context: [], quote: "Selected text" }, messages: [{ id: "answer", role: "assistant", content: "Done", turnId: "turn", createdAt: "2026-10-24" }], turns: [] });
  sessions.push({ ...sessions[0]!, id: "empty-side", title: "Main conversation · Side", side: { sourceId: "main", requestId: "empty", context: [], quote: "Selected text" } });
  const { ref } = setup();
  act(() => ref.current!.openSide("answered-side"));
  await screen.findByRole("textbox", { name: "Side draft answered-side" });
  expect(screen.queryByText("围绕选中的内容继续提问")).not.toBeInTheDocument();
  act(() => ref.current!.openSide("empty-side"));
  expect(await screen.findByText("围绕选中的内容继续提问")).toBeVisible();
});

it("isolates layout and selection across A → B → A and restores them after remount", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const view = setup();
  await act(async () => { await view.ref.current!.openResource("main", "https://a.example/guide"); });
  act(() => view.ref.current!.openSide("side-a"));
  const main = screen.getByRole("textbox", { name: "Main draft" });
  const draft = screen.getByRole("textbox", { name: "Side draft side-a" });
  fireEvent.change(draft, { target: { value: "Unsent A" } });
  view.select(b);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "关闭右侧面板" })).not.toBeInTheDocument();
  expect(main).toBe(screen.getByRole("textbox", { name: "Main draft" }));
  await act(async () => { await view.ref.current!.openResource(b.id, "https://b.example/"); });
  fireEvent.click(screen.getByRole("button", { name: "关闭右侧面板" }));
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: /旁支/ })).toHaveAttribute("aria-selected", "true");
  expect(screen.getByRole("textbox", { name: "Side draft side-a" })).toBe(draft);
  expect(draft).toHaveValue("Unsent A");
  view.select(b);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  act(() => view.ref.current!.toggle());
  expect(screen.getByRole("tab", { name: "b.example" })).toBeVisible();
  expect(screen.queryByRole("tab", { name: "a.example" })).not.toBeInTheDocument();
  view.select(sessions[0]!);
  view.unmount();
  const restored = setup();
  expect(screen.getByRole("tab", { name: /旁支/ })).toHaveAttribute("aria-selected", "true");
  restored.select(b);
  expect(screen.getByRole("tab", { name: "b.example" })).toBeVisible();
});

it("starts each new conversation with an empty closed panel and ignores the old shared layout", async () => {
  localStorage.setItem(`multica-side-workspace:${scope.userId}:${scope.workspaceId}`, JSON.stringify({ tabs: [{ id: "old", kind: "web", url: "https://old.example/" }], selected: "old", open: true }));
  const view = setup();
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  await act(async () => { await view.ref.current!.openResource("main", "https://a.example/"); });
  view.select(null);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  act(() => view.ref.current!.toggle());
  expect(screen.getByText("在这里继续旁支对话，或查看文件和网页")).toBeVisible();
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: "a.example" })).toBeVisible();
  view.select(null);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "关闭右侧面板" })).not.toBeInTheDocument();
});

it("keeps a late file result with its originating conversation", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  let finish!: (value: unknown) => void;
  request.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const view = setup();
  let pending!: Promise<void>;
  act(() => { pending = view.ref.current!.openResource("main", "/project/test.ts"); });
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  view.select(b);
  await act(async () => { finish(sessions[0]); await pending; });
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  view.select(sessions[0]!);
  expect(await screen.findByRole("tab", { name: "test.ts" })).toBeVisible();
});

it("uses the route session ID while its conversation summary is still loading", async () => {
  const view = setup();
  await act(async () => { await view.ref.current!.openResource("main", "https://a.example/"); });
  view.select(null);
  view.selectId("main");
  expect(screen.getByRole("tab", { name: "a.example" })).toBeVisible();
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: "a.example" })).toBeVisible();
});

it("releases the outgoing native webpage and restores only the selected conversation's view", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const view = setup();
  await act(async () => { await view.ref.current!.openResource("main", "https://a.example/"); });
  await waitFor(() => expect(preview.request.mock.calls.some(([, command]) => command.action === "show")).toBe(true));
  const id = preview.request.mock.calls.find(([, command]) => command.action === "show")![1].id;
  view.select(b);
  await waitFor(() => expect(preview.request).toHaveBeenCalledWith(scope, { id, action: "close" }));
  preview.request.mockClear();
  view.select(sessions[0]!);
  await waitFor(() => expect(preview.request.mock.calls.some(([, command]) => command.action === "show" && command.id === id)).toBe(true));
});

it.each([false, true])("routes a late Side creation to its original owner (new page: %s)", async (fromNew) => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const created = { ...sessions[0]!, id: "created-main" };
  const base = request.getMockImplementation()!;
  let finish!: (side: LocalConversation) => void;
  request.mockImplementation(async (scope, method, params) => {
    if (method === "create") { sessions.push(created); return created; }
    if (method === "sideCreate") return new Promise((resolve) => { finish = resolve; });
    return base(scope, method, params);
  });
  const view = setup();
  if (fromNew) view.select(null);
  act(() => view.ref.current!.toggle());
  fireEvent.click(screen.getByRole("button", { name: "新建旁支" }));
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  view.select(b);
  const origin = fromNew ? created : sessions[0]!;
  const side = { ...origin, id: "late-side", side: { sourceId: origin.id, requestId: "late", context: [] } }; sessions.push(side);
  await act(async () => { finish(side); });
  expect(view.onSelectSource).not.toHaveBeenCalled();
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  view.select(origin);
  expect(screen.getByRole("textbox", { name: "Side draft late-side" })).toBeVisible();
  expect(request).toHaveBeenCalledWith(scope, "sideCreate", expect.objectContaining({ id: origin.id }));
});

it("opens a newly created Side under its new main conversation and resets the next new page", async () => {
  const created = { ...sessions[0]!, id: "created-main" };
  const base = request.getMockImplementation()!;
  request.mockImplementation(async (scope, method, params) => {
    if (method === "create") { sessions.push(created); return created; }
    return base(scope, method, params);
  });
  const view = setup(); view.select(null);
  act(() => view.ref.current!.toggle());
  fireEvent.click(screen.getByRole("button", { name: "新建旁支" }));
  await waitFor(() => expect(view.onSelectSource).toHaveBeenCalledWith(created.id));
  view.select(created);
  expect(screen.getByRole("textbox", { name: "Side draft new-side" })).toBeVisible();
  view.select(null);
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "关闭右侧面板" })).not.toBeInTheDocument();
});

it("does not show an outgoing conversation's late file error in the current panel", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const base = request.getMockImplementation()!;
  let fail!: (reason: Error) => void;
  request.mockImplementation((scope, method, params) => method === "previewRead" ? new Promise((_resolve, reject) => { fail = reject; }) : base(scope, method, params));
  const view = setup();
  let pending!: Promise<void>;
  act(() => { pending = view.ref.current!.openResource("main", "/project/missing.ts"); });
  await waitFor(() => expect(fail).toBeTypeOf("function"));
  view.select(b); act(() => view.ref.current!.toggle());
  await act(async () => { fail(new Error("ENOENT: no such file")); await pending; });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
});

it("keeps a late native file picker result in its originating layout", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const base = request.getMockImplementation()!;
  let finish!: (value: unknown) => void;
  request.mockImplementation((scope, method, params) => method === "previewChoose" ? new Promise((resolve) => { finish = resolve; }) : base(scope, method, params));
  const view = setup(); act(() => view.ref.current!.toggle());
  fireEvent.click(screen.getByRole("button", { name: "添加右侧内容" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "打开文件" }));
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  view.select(b);
  await act(async () => { finish({ root: "/project", path: "/project/test.ts", name: "test.ts", kind: "file" }); });
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: "test.ts" })).toBeVisible();
});

it("keeps delayed main-transcript callbacks under the supplied source conversation", async () => {
  const b = { ...sessions[0]!, id: "main-b" }; sessions.push(b);
  const view = setup(); view.select(b);
  act(() => view.ref.current!.openSide("late-main-side", "main"));
  await act(async () => { await view.ref.current!.openResource("main", "https://a.example/", "main"); });
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  view.select(sessions[0]!);
  expect(screen.getByRole("tab", { name: "a.example" })).toBeVisible();
  expect(screen.getByRole("tab", { name: /旁支/ })).toBeVisible();
});

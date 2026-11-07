import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { ProjectChatsSection } from "./project-chats-section";

const scope = vi.hoisted(() => ({ wsId: "ws-1" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => scope.wsId }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ chat: () => `/${scope.wsId}/chat` }) }));
vi.mock("../../navigation/route-data-prefetch", () => ({ useRouteDataPrefetch: () => undefined }));
vi.mock("@multica/core/api", () => ({ api: { listChatSessions: vi.fn() } }));

const session = (id: string, fields: Partial<ChatSession> = {}): ChatSession => ({
  id, title: id, workspace_id: "ws-1", creator_id: "u1", agent_id: "a1",
  project_id: "p1", status: "active", has_unread: false,
  created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z", ...fields,
});

function setup(data?: ChatSession[], desktop = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (data) client.setQueryData(chatKeys.sessions("ws-1"), data);
  const push = vi.fn();
  const openInNewTab = vi.fn();
  const navigation = { pathname: "/ws-1/projects/p1", hash: "", searchParams: new URLSearchParams(), push,
    replace: vi.fn(), back: vi.fn(), getShareableUrl: (path: string) => `https://example.test${path}`,
    ...(desktop ? { openInNewTab } : {}),
  } as NavigationAdapter;
  const render = () => <QueryClientProvider client={client}><NavigationProvider value={navigation}><ProjectChatsSection projectId="p1" /></NavigationProvider></QueryClientProvider>;
  return { client, push, openInNewTab, render, ...renderWithI18n(render()) };
}

beforeEach(() => { scope.wsId = "ws-1"; vi.clearAllMocks(); });
describe("project Chats", () => {
  it("shows only active project chats, ranked by message activity rather than pin priority, with previews and links", () => {
    const { push } = setup([
      session("old-pinned", { pinned: true }),
      session("latest", { last_message: { content: "**Latest** summary\nnext", role: "assistant", created_at: "2026-10-04T00:00:00Z" } }),
      session("other", { project_id: "p2" }), session("unscoped", { project_id: null }),
      session("archived", { status: "archived" }), session("foreign", { workspace_id: "ws-2" }),
    ]);
    const links = screen.getAllByRole("link");
    expect(links.map((link) => link.getAttribute("href"))).toEqual(["/ws-1/chat?session=latest", "/ws-1/chat?session=old-pinned"]);
    expect(screen.getByText("Latest summary next")).toBeVisible();
    expect(links[0]!.querySelector("time")).toHaveAttribute("datetime", "2026-10-04T00:00:00Z");
    fireEvent.click(links[0]!);
    expect(push).toHaveBeenCalledWith("/ws-1/chat?session=latest");
  });

  it("uses the native app tab adapter on modifier click", () => {
    const { openInNewTab, push } = setup([session("native")], true);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://example.test/ws-1/chat?session=native");
    fireEvent.click(link, { ctrlKey: true });
    expect(openInNewTab).toHaveBeenCalledWith("/ws-1/chat?session=native", "native");
    expect(push).not.toHaveBeenCalled();
  });

  it("reacts to canonical cache reassociation, archive and workspace switches without a second cache", async () => {
    const { client, rerender, render } = setup([session("moving")]);
    act(() => { client.setQueryData(chatKeys.sessions("ws-1"), [session("moving", { project_id: null })]); });
    await screen.findByText("No active chats in this project");
    act(() => { client.setQueryData(chatKeys.sessions("ws-1"), [session("moving", { status: "archived" })]); });
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    client.setQueryData(chatKeys.sessions("ws-2"), [session("workspace-two", { workspace_id: "ws-2" })]);
    scope.wsId = "ws-2"; rerender(render());
    expect(await screen.findByRole("link")).toHaveAttribute("href", "/ws-2/chat?session=workspace-two");
    expect(screen.queryByText("moving")).not.toBeInTheDocument();
  });

  it("shows loading, failed retry and empty results", async () => {
    let reject!: (error: Error) => void;
    vi.mocked(api.listChatSessions).mockReturnValueOnce(new Promise((_resolve, rejectPromise) => { reject = rejectPromise; }));
    setup();
    expect(screen.getByRole("status")).toHaveTextContent("Loading chats...");
    await act(async () => { reject(new Error("offline")); });
    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't load chats");
    vi.mocked(api.listChatSessions).mockResolvedValue([]);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("No active chats in this project");
    await waitFor(() => expect(api.listChatSessions).toHaveBeenLastCalledWith({ status: "all" }));
  });
});

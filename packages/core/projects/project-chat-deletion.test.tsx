// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { chatKeys, chatSessionsOptions } from "../chat/queries";
import { useQuery } from "@tanstack/react-query";
import type { ChatSession } from "../types";
import { useDeleteProject } from "./mutations";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("../api", () => ({ api: { deleteProject: vi.fn(), listChatSessions: vi.fn() } }));
vi.mock("../chat/recent-context-store", () => ({ useRecentContextStore: { getState: () => ({ forgetContext: vi.fn() }) } }));
vi.mock("../issues/stores/surface-view-store", () => ({ clearIssueSurfaceViewState: vi.fn() }));

describe("project deletion chat cache", () => {
  it("refreshes surviving chat associations after server success, including inactive details, only in this workspace", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    const session = { id: "s1", project_id: "p1" } as ChatSession;
    client.setQueryData(chatKeys.sessions("ws-1"), [session]);
    client.setQueryData(chatKeys.session("ws-1", "s1"), session);
    client.setQueryData(chatKeys.sessions("ws-2"), [session]);
    vi.mocked(api.listChatSessions).mockResolvedValue([{ ...session, project_id: null }]);
    let finish!: () => void;
    vi.mocked(api.deleteProject).mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
    function Wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={client}>{children}</QueryClientProvider>; }
    const { result } = renderHook(() => ({ deletion: useDeleteProject(), chats: useQuery(chatSessionsOptions("ws-1")) }), { wrapper: Wrapper });
    act(() => result.current.deletion.mutate("p1"));
    await waitFor(() => expect(api.deleteProject).toHaveBeenCalledWith("p1"));
    expect(result.current.chats.data?.[0]?.project_id).toBe("p1");
    await act(async () => { finish(); });
    await waitFor(() => expect(result.current.chats.data?.[0]?.project_id).toBeNull());
    expect(client.getQueryState(chatKeys.session("ws-1", "s1"))?.isInvalidated).toBe(true);
    expect(client.getQueryState(chatKeys.sessions("ws-2"))?.isInvalidated).toBe(false);
  });
});

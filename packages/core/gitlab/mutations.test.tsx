// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import {
  useConnectWorkspaceGitlabMutation,
  useDisconnectWorkspaceGitlabMutation,
} from "./mutations";

vi.mock("../api", () => ({
  api: {
    getWorkspaceGitlabConnection: vi.fn(),
    connectWorkspaceGitlab: vi.fn(),
    disconnectWorkspaceGitlab: vi.fn(),
  },
}));

import { api } from "../api";

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

describe("gitlab mutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("connectWorkspaceGitlab calls the api and caches the response", async () => {
    const connection = {
      workspace_id: "ws-1",
      gitlab_project_id: 1,
      gitlab_project_path: "g/a",
      service_token_user_id: 1,
      connection_status: "connected" as const,
    };
    (api.connectWorkspaceGitlab as ReturnType<typeof vi.fn>).mockResolvedValue(connection);

    const { result } = renderHook(() => useConnectWorkspaceGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync({ project: "1", token: "t" });
    });
    expect(api.connectWorkspaceGitlab).toHaveBeenCalledWith("ws-1", { project: "1", token: "t" });
    await waitFor(() => expect(result.current.data).toEqual(connection));
  });

  it("disconnectWorkspaceGitlab calls the api", async () => {
    (api.disconnectWorkspaceGitlab as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    const { result } = renderHook(() => useDisconnectWorkspaceGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync();
    });
    expect(api.disconnectWorkspaceGitlab).toHaveBeenCalledWith("ws-1");
  });
});

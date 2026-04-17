import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { GitlabTab } from "./gitlab-tab";
import { ApiError } from "@multica/core/api";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      getWorkspaceGitlabConnection: vi.fn(),
      connectWorkspaceGitlab: vi.fn(),
      disconnectWorkspaceGitlab: vi.fn(),
    },
  };
});

import { api } from "@multica/core/api";

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <GitlabTab />
    </QueryClientProvider>,
  );
}

describe("GitlabTab", () => {
  it("shows the connect form when not connected (404 from GET)", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError("gitlab is not connected", 404, "Not Found"),
    );
    renderTab();
    expect(await screen.findByRole("heading", { name: /connect gitlab/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/project/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/token/i)).toBeInTheDocument();
  });

  it("submits the form and shows connected state", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError("gitlab is not connected", 404, "Not Found"),
    );
    (api.connectWorkspaceGitlab as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 7,
      gitlab_project_path: "team/app",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    renderTab();
    await userEvent.type(await screen.findByLabelText(/project/i), "team/app");
    await userEvent.type(screen.getByLabelText(/token/i), "glpat-abc");
    await userEvent.click(screen.getByRole("button", { name: /connect/i }));

    await waitFor(() => {
      expect(screen.getByText(/team\/app/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
    });
  });

  it("shows connected state when already connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/repo",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    renderTab();
    expect(await screen.findByText(/group\/repo/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
  });
});

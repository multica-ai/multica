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
      getUserGitlabConnection: vi.fn(),
      connectUserGitlab: vi.fn(),
      disconnectUserGitlab: vi.fn(),
    },
  };
});

import { api } from "@multica/core/api";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <GitlabTab />
    </QueryClientProvider>,
  );
}

// Keep old helper name for backwards compat with existing tests
function renderTab() {
  return renderPage();
}

describe("GitlabTab", () => {
  it("shows the connect form when not connected (404 from GET)", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError("gitlab is not connected", 404, "Not Found"),
    );
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderTab();
    expect(await screen.findByRole("heading", { name: /connect gitlab/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/^project$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/service access token/i)).toBeInTheDocument();
  });

  it("renders the personal PAT section even when the workspace is not connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError("gitlab is not connected", 404, "Not Found"),
    );
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderPage();

    // Workspace-side connect form still renders.
    expect(await screen.findByRole("heading", { name: /connect gitlab/i })).toBeInTheDocument();

    // Personal section also renders so users can manage their PAT independently.
    expect(
      await screen.findByRole("heading", { name: /your personal gitlab connection/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/connect your personal gitlab account/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/personal access token/i)).toBeInTheDocument();
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
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderTab();
    await userEvent.type(await screen.findByLabelText(/^project$/i), "team/app");
    await userEvent.type(screen.getByLabelText(/service access token/i), "glpat-abc");
    await userEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    await waitFor(() => {
      expect(screen.getByText(/team\/app/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
    });
  });

  it("shows project info + disconnect button when connection_status is 'error' (no dead-end)", async () => {
    // Regression: a failed webhook registration leaves the row at status=error
    // with a status_message. The UI used to render the connect form in this
    // state, trapping the user — they couldn't disconnect without direct DB
    // access. Now we render the connected-state shell with the error banner.
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/broken-repo",
      service_token_user_id: 1,
      connection_status: "error",
      status_message: "Failed to register GitLab webhook: forbidden (403).",
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderTab();
    expect(await screen.findByText(/group\/broken-repo/)).toBeInTheDocument();
    // The Disconnect button must be reachable so the user can reset.
    expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
    // The error message is surfaced as a banner.
    expect(screen.getByRole("alert")).toHaveTextContent(/forbidden \(403\)/);
    // The connect form (heading) must NOT render — we have a row already.
    expect(screen.queryByRole("heading", { name: /connect gitlab/i })).not.toBeInTheDocument();
  });

  it("shows connected state when already connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/repo",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderTab();
    expect(await screen.findByText(/group\/repo/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /disconnect/i })).toBeInTheDocument();
  });

  it("renders the personal connection form when workspace connected + user not connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/repo",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({ connected: false });
    renderPage();
    expect(await screen.findByText(/connect your personal gitlab/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/personal access token/i)).toBeInTheDocument();
  });

  it("renders 'connected as @username' when user has connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      workspace_id: "ws-1",
      gitlab_project_id: 9,
      gitlab_project_path: "group/repo",
      service_token_user_id: 1,
      connection_status: "connected",
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      connected: true, gitlab_username: "alice",
    });
    renderPage();
    expect(await screen.findByText(/@alice/)).toBeInTheDocument();
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { GitlabPatBanner } from "./gitlab-pat-banner";

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "my-team" }),
  };
});

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      getWorkspaceGitlabConnection: vi.fn(),
      getUserGitlabConnection: vi.fn(),
    },
  };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

import { api } from "@multica/core/api";

function renderBanner() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <GitlabPatBanner />
    </QueryClientProvider>,
  );
}

describe("GitlabPatBanner", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("renders when workspace connected + user not connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      gitlab_project_id: 7,
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      connected: false,
    });
    renderBanner();
    expect(
      await screen.findByText(/connect your gitlab account/i),
    ).toBeInTheDocument();
  });

  it("hides when user is already connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      gitlab_project_id: 7,
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      connected: true,
    });
    const { container } = renderBanner();
    // Wait a tick for queries to settle then assert empty
    await vi.waitFor(() => {
      expect(container).toBeEmptyDOMElement();
    });
  });

  it("hides when workspace is not connected", async () => {
    (api.getWorkspaceGitlabConnection as ReturnType<typeof vi.fn>).mockRejectedValue({
      status: 404,
    });
    (api.getUserGitlabConnection as ReturnType<typeof vi.fn>).mockResolvedValue({
      connected: false,
    });
    const { container } = renderBanner();
    await vi.waitFor(() => {
      expect(container).toBeEmptyDOMElement();
    });
  });
});

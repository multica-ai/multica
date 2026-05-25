import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { paths } from "@multica/core/paths";

const { mockReplace, authState } = vi.hoisted(() => ({
  mockReplace: vi.fn(),
  authState: {
    user: null as null | { preferences?: Record<string, unknown> },
    isLoading: false,
  },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mockReplace }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) =>
    selector(authState),
}));

import WorkspaceRootPage from "./page";
import { WorkspaceRootRedirect } from "./workspace-root-redirect";

describe("WorkspaceRootPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authState.user = null;
    authState.isLoading = false;
    window.innerWidth = 1024;
  });

  it("passes the bare workspace slug to the client redirect", async () => {
    const page = await WorkspaceRootPage({
      params: Promise.resolve({ workspaceSlug: "jeh-b0edd870" }),
    });

    expect(page).toEqual(
      <WorkspaceRootRedirect workspaceSlug="jeh-b0edd870" />,
    );
  });

  it("redirects the bare workspace slug to the desktop start page preference", async () => {
    authState.user = { preferences: { start_page_desktop: "projects" } };
    render(<WorkspaceRootRedirect workspaceSlug="jeh-b0edd870" />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/jeh-b0edd870/projects");
    });
  });

  it("redirects the bare workspace slug to the mobile start page preference", async () => {
    window.innerWidth = 390;
    authState.user = { preferences: { start_page_mobile: "my-issues" } };
    render(<WorkspaceRootRedirect workspaceSlug="jeh-b0edd870" />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith("/jeh-b0edd870/my-issues");
    });
  });

  it("falls back to issues when no start page preference is set", async () => {
    authState.user = { preferences: {} };
    render(<WorkspaceRootRedirect workspaceSlug="jeh-b0edd870" />);

    await waitFor(() => {
      expect(mockReplace).toHaveBeenCalledWith(
        paths.workspace("jeh-b0edd870").issues(),
      );
    });
  });

  it("waits for auth before redirecting", async () => {
    authState.isLoading = true;
    authState.user = null;
    render(<WorkspaceRootRedirect workspaceSlug="jeh-b0edd870" />);

    expect(mockReplace).not.toHaveBeenCalled();
  });
});

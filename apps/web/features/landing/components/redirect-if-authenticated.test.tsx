import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { documentNavigation } from "@/platform/web-host-path";

const state = vi.hoisted(() => ({
  user: { id: "user-1" } as { id: string } | null,
  isLoading: false,
  workspaces: [] as { id: string; slug: string }[],
  ready: false,
  replace: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (
    selector: (auth: {
      user: typeof state.user;
      isLoading: boolean;
    }) => unknown,
  ) => selector({ user: state.user, isLoading: state.isLoading }),
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({
    workspaces: state.workspaces,
    ready: state.ready,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: (workspaces: { slug: string }[]) =>
    workspaces[0] ? `/${workspaces[0].slug}/issues` : "/workspaces/new",
}));

vi.mock("@/lib/public-host", () => ({
  isOfficialMarketingHost: () => false,
}));

import { RedirectIfAuthenticated } from "./redirect-if-authenticated";

beforeEach(() => {
  vi.spyOn(documentNavigation, "assign").mockImplementation(() => {});
  vi.spyOn(documentNavigation, "replace").mockImplementation(() => {});
  state.user = { id: "user-1" };
  state.isLoading = false;
  state.workspaces = [];
  state.ready = false;
  state.replace.mockReset();
});

describe("RedirectIfAuthenticated", () => {
  it("does not redirect after an initial workspace-list failure", () => {
    render(<RedirectIfAuthenticated />);

    expect(state.replace).not.toHaveBeenCalled();
  });

  it("redirects only after an authoritative workspace list arrives", async () => {
    state.ready = true;
    state.workspaces = [{ id: "ws-1", slug: "acme" }];

    render(<RedirectIfAuthenticated />);

    await waitFor(() => {
      expect(documentNavigation.replace).toHaveBeenCalledWith("/tag/acme/chat");
      expect(state.replace).not.toHaveBeenCalled();
    });
  });

  it("hands an empty Workspace list to the Tag authority create route", async () => {
    state.ready = true;

    render(<RedirectIfAuthenticated />);

    await waitFor(() => {
      expect(documentNavigation.replace).toHaveBeenCalledWith(
        "/tag/workspaces/new",
      );
      expect(state.replace).not.toHaveBeenCalled();
    });
  });
});

import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

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
    selector: (auth: { user: typeof state.user; isLoading: boolean }) => unknown,
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
      expect(state.replace).toHaveBeenCalledWith("/acme/issues");
    });
  });
});

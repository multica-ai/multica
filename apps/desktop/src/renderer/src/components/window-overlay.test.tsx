import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

// Shared hoisted state so each test can set the active overlay + user then
// render. Mirrors the store-mock pattern in pageview-tracker.test.tsx.
const state = vi.hoisted(() => ({
  overlay: null as { type: string; invitationId?: string } | null,
  user: null as { email: string } | null,
  logout: vi.fn<() => void>(),
  push: vi.fn<(path: string) => void>(),
  close: vi.fn<() => void>(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
}));

// Heavy shared views are irrelevant to the escape affordance — stub them so
// the test stays focused on the desktop overlay wiring.
vi.mock("@multica/views/workspace/new-workspace-page", () => ({
  NewWorkspacePage: () => <div data-testid="new-workspace" />,
}));
vi.mock("@multica/views/invite", () => ({
  InvitePage: () => <div data-testid="invite" />,
}));
vi.mock("@multica/views/invitations", () => ({
  InvitationsPage: () => <div data-testid="invitations" />,
}));
vi.mock("@multica/views/onboarding", () => ({
  OnboardingFlow: () => <div data-testid="onboarding-flow" />,
}));
vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: state.push }),
}));
vi.mock("@multica/views/auth", () => ({
  useLogout: () => state.logout,
}));
vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useAuthStore };
});
vi.mock("@multica/core/paths", () => ({
  paths: {
    root: () => "/",
    workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }),
  },
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@/stores/window-overlay-store", () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useWindowOverlayStore };
});

import { WindowOverlay } from "./window-overlay";

describe("WindowOverlay onboarding escape", () => {
  beforeEach(() => {
    state.overlay = null;
    state.user = null;
    state.logout.mockReset();
  });

  it("renders a Log out escape + the signed-in email on the onboarding overlay", () => {
    state.overlay = { type: "onboarding" };
    state.user = { email: "wrong-account@gmail.com" };
    render(<WindowOverlay />);

    expect(screen.getByTestId("onboarding-flow")).toBeTruthy();
    expect(screen.getByText("wrong-account@gmail.com")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /log out/i }),
    ).toBeTruthy();
  });

  it("logs out when the escape button is clicked", () => {
    state.overlay = { type: "onboarding" };
    state.user = { email: "wrong-account@gmail.com" };
    render(<WindowOverlay />);

    fireEvent.click(screen.getByRole("button", { name: /log out/i }));
    expect(state.logout).toHaveBeenCalledTimes(1);
  });

  it("does not render the onboarding escape on other overlays", () => {
    state.overlay = { type: "new-workspace" };
    render(<WindowOverlay />);

    expect(screen.getByTestId("new-workspace")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /log out/i })).toBeNull();
  });
});

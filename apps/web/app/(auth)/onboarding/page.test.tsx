import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

// FIR-252: the web /onboarding route must stop rendering the upstream
// OnboardingFlow when `cerebro_disable_onboarding` is on (and the Firtal
// welcome page is not handling the user) — it redirects instead, so a
// 0-workspace user lands on /workspaces/new (which always has Log out)
// rather than the create-workspace trap. With the flag off, the upstream
// flow is preserved.

const state = vi.hoisted(() => ({
  user: { id: "u1", email: "a@b.dk", onboarded_at: null as string | null },
  isLoading: false,
  workspaces: [] as { id: string; slug: string }[],
  workspacesFetched: true,
  disableOnboarding: true,
  firtalWelcome: false,
  replace: vi.fn<(path: string) => void>(),
  push: vi.fn<(path: string) => void>(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace, push: state.push }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.workspaces, isFetched: state.workspacesFetched }),
}));

// Real @multica/core/paths (resolvePostAuthDestination + paths are pure);
// useHasOnboarded reads the mocked auth store below.
vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useAuthStore };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));

vi.mock("@multica/views/onboarding", () => ({
  OnboardingFlow: () => <div data-testid="onboarding-flow" />,
  CliInstallInstructions: () => <div data-testid="cli-install" />,
}));
vi.mock("@multica/views/auth", () => ({ useLogout: () => vi.fn() }));
vi.mock("@multica/cerebro-firtal-welcome", () => ({
  FirtalWelcomePage: () => <div data-testid="firtal-welcome" />,
  useFirtalWelcomeEnabled: () => state.firtalWelcome,
  markFirtalWelcomeSeen: vi.fn(),
}));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFlagValue: (key: string) =>
    key === "cerebro_disable_onboarding" ? state.disableOnboarding : false,
}));

import OnboardingPage from "./page";

describe("OnboardingPage — cerebro_disable_onboarding", () => {
  beforeEach(() => {
    state.user = { id: "u1", email: "a@b.dk", onboarded_at: null };
    state.workspaces = [];
    state.workspacesFetched = true;
    state.disableOnboarding = true;
    state.firtalWelcome = false;
    state.replace.mockReset();
    state.push.mockReset();
  });

  it("redirects a 0-workspace user to /workspaces/new and never renders OnboardingFlow", async () => {
    render(<OnboardingPage />);
    await waitFor(() => expect(state.replace).toHaveBeenCalled());
    expect(state.replace.mock.calls[0][0]).toContain("/workspaces/new");
    expect(screen.queryByTestId("onboarding-flow")).toBeNull();
  });

  it("routes a user who already has a workspace straight into it", async () => {
    state.workspaces = [{ id: "w1", slug: "firtal" }];
    render(<OnboardingPage />);
    await waitFor(() => expect(state.replace).toHaveBeenCalled());
    expect(state.replace.mock.calls[0][0]).toContain("firtal");
    expect(screen.queryByTestId("onboarding-flow")).toBeNull();
  });

  it("preserves the upstream OnboardingFlow when the flag is off", async () => {
    state.disableOnboarding = false;
    render(<OnboardingPage />);
    expect(await screen.findByTestId("onboarding-flow")).toBeTruthy();
    expect(state.replace).not.toHaveBeenCalled();
  });

  it("still shows the Firtal welcome page when its flag is on (not bypassed)", async () => {
    state.firtalWelcome = true;
    render(<OnboardingPage />);
    expect(await screen.findByTestId("firtal-welcome")).toBeTruthy();
    expect(state.replace).not.toHaveBeenCalled();
    expect(screen.queryByTestId("onboarding-flow")).toBeNull();
  });
});

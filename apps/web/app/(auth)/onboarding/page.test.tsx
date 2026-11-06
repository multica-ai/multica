import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  user: { id: "user-1" } as { id: string } | null,
  onboarded: false,
  expired: false,
  destination: { kind: "issue", issueId: "ACME-1" } as
    | { kind: "issue"; issueId: string }
    | { kind: "chat"; sessionId: string },
  push: vi.fn(),
  replace: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: state.push, replace: state.replace }),
  usePathname: () => "/onboarding",
}));

vi.mock("@multica/core/auth", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/auth")>(),
  useAuthStore: (select: (auth: { user: typeof state.user; isLoading: boolean; expired: boolean }) => unknown) =>
    select({ user: state.user, isLoading: false, expired: state.expired }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/paths")>(),
  useHasOnboarded: () => state.onboarded,
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ workspaces: [{ slug: "acme" }], ready: true }),
}));

vi.mock("@multica/views/onboarding", () => ({
  CliInstallInstructions: () => null,
  OnboardingFlow: ({ onComplete }: { onComplete: (workspace: { slug: string }, destination: typeof state.destination) => void }) => (
    <button onClick={() => onComplete({ slug: "acme" }, state.destination)}>
      Finish setup
    </button>
  ),
}));

import OnboardingPage from "./page";

const target = "/acme/projects/project-1?view=members#activity";
const original = window.location.href;

beforeEach(() => {
  vi.clearAllMocks();
  state.user = { id: "user-1" };
  state.onboarded = false;
  state.expired = false;
  state.destination = { kind: "issue", issueId: "ACME-1" };
  window.history.replaceState(null, "", `/onboarding?next=${encodeURIComponent(target)}`);
});

afterEach(() => window.history.replaceState(null, "", original));

describe("onboarding return target", () => {
  it.each(["issue", "chat"] as const)("prefers the original link to the setup %s destination", (kind) => {
    state.destination = kind === "issue"
      ? { kind, issueId: "ACME-1" }
      : { kind, sessionId: "session-1" };
    render(<OnboardingPage />);
    fireEvent.click(screen.getByRole("button", { name: "Finish setup" }));
    expect(state.replace).toHaveBeenCalledExactlyOnceWith(target);
    expect(state.push).not.toHaveBeenCalled();
  });

  it.each([
    [{ kind: "issue", issueId: "ACME-1" }, "/acme/issues/ACME-1"],
    [{ kind: "chat", sessionId: "session-1" }, "/acme/chat?session=session-1"],
  ] as const)("uses the setup destination without an original link (%j)", (destination, href) => {
    state.destination = destination;
    window.history.replaceState(null, "", "/onboarding");
    render(<OnboardingPage />);
    fireEvent.click(screen.getByRole("button", { name: "Finish setup" }));
    expect(state.push).toHaveBeenCalledExactlyOnceWith(href);
    expect(state.replace).not.toHaveBeenCalled();
  });

  it("discards the previous target when signing out during setup", () => {
    const { rerender } = render(<OnboardingPage />);
    state.user = null;
    rerender(<OnboardingPage />);
    expect(state.replace).toHaveBeenCalledExactlyOnceWith("/login");
  });

  it("resumes the original link if setup was already completed", () => {
    state.onboarded = true;
    render(<OnboardingPage />);
    expect(state.replace).toHaveBeenCalledExactlyOnceWith(target);
  });

  it("retains the onboarding link if the session expires", () => {
    state.user = null;
    const onboarding = window.location.pathname + window.location.search;
    render(<OnboardingPage />);
    expect(state.replace).toHaveBeenCalledExactlyOnceWith(`/login?next=${encodeURIComponent(onboarding)}`);
  });

  it.each(["//example.com", "/onboarding"])("uses the setup guide for invalid return target %s", (next) => {
    window.history.replaceState(null, "", `/onboarding?next=${encodeURIComponent(next)}`);
    render(<OnboardingPage />);
    fireEvent.click(screen.getByRole("button", { name: "Finish setup" }));
    expect(state.push).toHaveBeenCalledExactlyOnceWith("/acme/issues/ACME-1");
    expect(state.replace).not.toHaveBeenCalled();
  });
});

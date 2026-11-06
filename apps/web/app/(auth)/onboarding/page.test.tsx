import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  user: { id: "user-1" } as { id: string } | null,
  onboarded: false,
  push: vi.fn(),
  replace: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: state.push, replace: state.replace }),
}));

vi.mock("@multica/core/auth", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/auth")>(),
  useAuthStore: (select: (auth: { user: typeof state.user; isLoading: boolean }) => unknown) =>
    select({ user: state.user, isLoading: false }),
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
  OnboardingFlow: ({ onComplete }: { onComplete: (workspace: { slug: string }, destination: { kind: "issue"; issueId: string }) => void }) => (
    <button onClick={() => onComplete({ slug: "acme" }, { kind: "issue", issueId: "ACME-1" })}>
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
  window.history.replaceState(null, "", `/onboarding?next=${encodeURIComponent(target)}`);
});

afterEach(() => window.history.replaceState(null, "", original));

describe("onboarding return target", () => {
  it("opens the original link after completing setup", () => {
    render(<OnboardingPage />);
    fireEvent.click(screen.getByRole("button", { name: "Finish setup" }));
    expect(state.replace).toHaveBeenCalledExactlyOnceWith(target);
    expect(state.push).not.toHaveBeenCalled();
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

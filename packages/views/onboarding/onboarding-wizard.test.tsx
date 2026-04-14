import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockWorkspaceState = vi.hoisted(() => ({
  workspace: null as { id: string } | null,
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: (state: typeof mockWorkspaceState) => unknown) =>
      selector ? selector(mockWorkspaceState) : mockWorkspaceState,
    {
      getState: () => mockWorkspaceState,
    },
  ),
}));

vi.mock("./step-workspace", () => ({
  StepWorkspace: ({ onNext }: { onNext: () => void }) => (
    <button type="button" onClick={onNext}>
      Finish workspace
    </button>
  ),
}));

vi.mock("./step-runtime", () => ({
  StepRuntime: ({ wsId }: { wsId: string }) => (
    <div>Runtime step for {wsId}</div>
  ),
}));

vi.mock("./step-agent", () => ({
  StepAgent: () => <div>Agent step</div>,
}));

vi.mock("./step-complete", () => ({
  StepComplete: () => <div>Complete step</div>,
}));

import { OnboardingWizard } from "./onboarding-wizard";

describe("OnboardingWizard", () => {
  beforeEach(() => {
    mockWorkspaceState.workspace = null;
  });

  it("starts at workspace creation when no workspace exists", () => {
    render(<OnboardingWizard onComplete={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Finish workspace" }),
    ).toBeInTheDocument();
  });

  it("continues setup when a workspace already exists", () => {
    mockWorkspaceState.workspace = { id: "ws-123" };

    render(<OnboardingWizard onComplete={vi.fn()} />);

    expect(screen.getByText("Runtime step for ws-123")).toBeInTheDocument();
  });

  it("continues setup when the workspace becomes available after mount", async () => {
    const { rerender } = render(<OnboardingWizard onComplete={vi.fn()} />);

    expect(
      screen.getByRole("button", { name: "Finish workspace" }),
    ).toBeInTheDocument();

    mockWorkspaceState.workspace = { id: "ws-456" };
    rerender(<OnboardingWizard onComplete={vi.fn()} />);

    expect(
      await screen.findByText("Runtime step for ws-456"),
    ).toBeInTheDocument();
  });

  it("does not skip runtime when workspace creation also switches workspace", () => {
    render(<OnboardingWizard onComplete={vi.fn()} />);

    mockWorkspaceState.workspace = { id: "ws-789" };
    fireEvent.click(screen.getByRole("button", { name: "Finish workspace" }));

    expect(screen.getByText("Runtime step for ws-789")).toBeInTheDocument();
    expect(screen.queryByText("Agent step")).not.toBeInTheDocument();
  });
});

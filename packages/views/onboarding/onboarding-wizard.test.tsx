import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  type QueryClient as QC,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Workspace } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";

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

// Stub the list query so the wizard reads whatever we seeded in the cache.
// `listWorkspaces` returns a promise that never resolves so the seeded cache
// data isn't overwritten by a background refetch during the test.
vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: vi.fn(() => new Promise(() => {})),
  },
}));

import { OnboardingWizard } from "./onboarding-wizard";

function makeWorkspace(id: string, slug = id): Workspace {
  return {
    id,
    name: id,
    slug,
    created_at: "",
    updated_at: "",
  } as Workspace;
}

function renderWithCache(
  wsList: Workspace[],
  onComplete = vi.fn(),
): { qc: QC } {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(workspaceKeys.list(), wsList);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  render(<OnboardingWizard onComplete={onComplete} />, { wrapper });
  return { qc };
}

describe("OnboardingWizard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("starts at workspace creation when no workspace exists", () => {
    renderWithCache([]);

    expect(
      screen.getByRole("button", { name: "Finish workspace" }),
    ).toBeInTheDocument();
  });

  it("continues setup when a workspace already exists", () => {
    renderWithCache([makeWorkspace("ws-123")]);

    expect(screen.getByText("Runtime step for ws-123")).toBeInTheDocument();
  });

  it("continues setup when the workspace becomes available after mount", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(workspaceKeys.list(), []);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );

    render(<OnboardingWizard onComplete={vi.fn()} />, { wrapper });

    expect(
      screen.getByRole("button", { name: "Finish workspace" }),
    ).toBeInTheDocument();

    // Simulate useCreateWorkspace adding the new workspace to the list cache.
    qc.setQueryData(workspaceKeys.list(), [makeWorkspace("ws-456")]);

    expect(
      await screen.findByText("Runtime step for ws-456"),
    ).toBeInTheDocument();
  });

  it("does not skip runtime when workspace creation also advances step", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    qc.setQueryData(workspaceKeys.list(), []);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );

    render(<OnboardingWizard onComplete={vi.fn()} />, { wrapper });

    // Mutation's onSuccess populates the cache first, then the step-workspace
    // mock calls onNext — we should land on step 1 (runtime), never step 2.
    qc.setQueryData(workspaceKeys.list(), [makeWorkspace("ws-789")]);
    fireEvent.click(screen.getByRole("button", { name: "Finish workspace" }));

    expect(screen.getByText("Runtime step for ws-789")).toBeInTheDocument();
    expect(screen.queryByText("Agent step")).not.toBeInTheDocument();
  });
});

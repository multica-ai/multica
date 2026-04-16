import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { OnboardingGate } from "./onboarding-gate";

// Prevent actual API calls — the tests seed data via setQueryData.
vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: vi.fn().mockResolvedValue([]),
  },
}));

function createTestQueryClient(
  workspaces: Array<{ id: string; slug: string }> = [],
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // Seed the workspace list so the gate can read it synchronously.
  qc.setQueryData(workspaceKeys.list(), workspaces);
  return qc;
}

function renderGate(
  qc: QueryClient,
  onboarding?: (onComplete: () => void) => React.ReactNode,
) {
  return render(
    <QueryClientProvider client={qc}>
      <OnboardingGate
        onboarding={
          onboarding ??
          ((onComplete) => (
            <button type="button" data-testid="finish" onClick={onComplete}>
              wizard
            </button>
          ))
        }
      >
        <div data-testid="main">main shell</div>
      </OnboardingGate>
    </QueryClientProvider>,
  );
}

describe("OnboardingGate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders children when workspaces exist in cache", () => {
    const qc = createTestQueryClient([{ id: "ws-1", slug: "my-team" }]);
    renderGate(qc);

    expect(screen.getByTestId("main")).toBeInTheDocument();
    expect(screen.queryByText("wizard")).not.toBeInTheDocument();
  });

  it("renders onboarding when workspace list is empty", () => {
    const qc = createTestQueryClient([]);
    renderGate(qc);

    expect(screen.getByText("wizard")).toBeInTheDocument();
    expect(screen.queryByTestId("main")).not.toBeInTheDocument();
  });

  it("keeps the wizard mounted even after workspaces appear in cache mid-flow", () => {
    const qc = createTestQueryClient([]);
    renderGate(qc);

    expect(screen.getByText("wizard")).toBeInTheDocument();

    // Simulate the onboarding wizard creating a workspace mid-flow.
    act(() => {
      qc.setQueryData(workspaceKeys.list(), [
        { id: "ws-new", slug: "new-team" },
      ]);
    });

    // Wizard should still be visible — only onComplete dismisses it.
    expect(screen.getByText("wizard")).toBeInTheDocument();
    expect(screen.queryByTestId("main")).not.toBeInTheDocument();
  });

  it("transitions to children after the wizard calls onComplete", () => {
    const qc = createTestQueryClient([]);
    renderGate(qc);

    expect(screen.getByTestId("finish")).toBeInTheDocument();

    act(() => {
      screen.getByTestId("finish").click();
    });

    expect(screen.getByTestId("main")).toBeInTheDocument();
    expect(screen.queryByTestId("finish")).not.toBeInTheDocument();
  });
});

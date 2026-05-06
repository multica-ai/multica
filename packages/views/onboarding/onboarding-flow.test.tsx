// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  LOCAL_PRODUCT_CAPABILITIES,
  type ProductCapabilities,
} from "@multica/core/config";
import { ProductCapabilitiesProvider } from "@multica/core/platform";

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockUser = {
  id: "user-1",
  email: "u@example.com",
  name: "User",
  onboarding_questionnaire: {},
};

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (s: { user: typeof mockUser }) => unknown) => {
    const state = { user: mockUser };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/onboarding", () => ({
  ONBOARDING_STEP_ORDER: [
    "welcome",
    "questionnaire",
    "workspace",
    "runtime",
    "agent",
    "first_issue",
  ],
  saveQuestionnaire: vi.fn().mockResolvedValue(undefined),
  completeOnboarding: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspaces", "list"],
    queryFn: () => Promise.resolve([]),
  }),
  workspaceKeys: {
    agents: (wsId: string) => ["workspaces", wsId, "agents"],
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isFetched: true }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/platform", async () => {
  const actual = await vi.importActual<
    typeof import("@multica/core/platform")
  >("@multica/core/platform");
  return {
    ...actual,
    setCurrentWorkspace: vi.fn(),
  };
});

vi.mock("@multica/views/platform", () => ({
  DragStrip: () => <div data-testid="drag-strip" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Mock step components — each exposes a button that triggers the relevant
// callback so tests can drive the flow forward step-by-step.
vi.mock("./components/step-header", () => ({
  StepHeader: () => <div data-testid="step-header" />,
}));

vi.mock("./steps/step-welcome", () => ({
  StepWelcome: ({ onNext }: { onNext: () => void }) => (
    <button data-testid="welcome-next" onClick={onNext}>
      welcome-next
    </button>
  ),
}));

vi.mock("./steps/step-questionnaire", () => ({
  StepQuestionnaire: ({
    onSubmit,
  }: {
    onSubmit: (answers: Record<string, null>) => void | Promise<void>;
  }) => (
    <button
      data-testid="questionnaire-next"
      onClick={() =>
        onSubmit({
          team_size: null,
          team_size_other: null,
          role: null,
          role_other: null,
          use_case: null,
          use_case_other: null,
        })
      }
    >
      questionnaire-next
    </button>
  ),
}));

vi.mock("./steps/step-workspace", () => ({
  StepWorkspace: ({
    onCreated,
  }: {
    onCreated: (ws: { id: string; slug: string; name: string }) => void;
  }) => (
    <button
      data-testid="workspace-next"
      onClick={() =>
        onCreated({ id: "ws-1", slug: "demo", name: "Demo Workspace" })
      }
    >
      workspace-next
    </button>
  ),
}));

vi.mock("./steps/step-runtime-connect", () => ({
  StepRuntimeConnect: () => <div data-testid="step-runtime-connect" />,
}));

vi.mock("./steps/step-platform-fork", () => ({
  StepPlatformFork: () => <div data-testid="step-platform-fork" />,
}));

vi.mock("./steps/step-agent", () => ({
  StepAgent: () => <div data-testid="step-agent" />,
}));

vi.mock("./steps/step-first-issue", () => ({
  StepFirstIssue: () => <div data-testid="step-first-issue" />,
}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { OnboardingFlow } from "./onboarding-flow";

const cloudCaps: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  runtimes: {
    ...LOCAL_PRODUCT_CAPABILITIES.runtimes,
    allowCloud: true,
  },
};

// Drive the flow from welcome → runtime by clicking through the mocked
// buttons each step exposes.
async function advanceToRuntimeStep() {
  fireEvent.click(await screen.findByTestId("welcome-next"));
  fireEvent.click(await screen.findByTestId("questionnaire-next"));
  fireEvent.click(await screen.findByTestId("workspace-next"));
}

describe("OnboardingFlow capability gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders StepRuntimeConnect (not StepPlatformFork) in local mode even when runtimeInstructions is provided", async () => {
    render(
      <OnboardingFlow
        onComplete={() => {}}
        runtimeInstructions={<div data-testid="cli-instructions" />}
      />,
    );

    await advanceToRuntimeStep();

    expect(
      await screen.findByTestId("step-runtime-connect"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("step-platform-fork")).toBeNull();
  });

  it("renders StepPlatformFork when allowCloud is true and runtimeInstructions is provided", async () => {
    render(
      <ProductCapabilitiesProvider capabilities={cloudCaps}>
        <OnboardingFlow
          onComplete={() => {}}
          runtimeInstructions={<div data-testid="cli-instructions" />}
        />
      </ProductCapabilitiesProvider>,
    );

    await advanceToRuntimeStep();

    expect(
      await screen.findByTestId("step-platform-fork"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("step-runtime-connect")).toBeNull();
  });
});

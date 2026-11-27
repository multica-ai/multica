import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { ApiError } from "@multica/core/api";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";
import enWorkspace from "../locales/en/workspace.json";

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

const { mockCreateIssue, mockCompleteOnboarding, mockCreatePersonalAgent, mockToastError } =
  vi.hoisted(() => ({
    mockCreateIssue: vi.fn(),
    mockCompleteOnboarding: vi.fn(),
    mockCreatePersonalAgent: vi.fn(),
    mockToastError: vi.fn(),
  }));

vi.mock("sonner", () => ({ toast: { error: mockToastError } }));

vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      getBaseUrl: () => "https://multica.ai",
      createIssue: (...args: unknown[]) => mockCreateIssue(...args),
    },
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) =>
      selector({ user: { id: "u-1", name: "Ada", onboarding_questionnaire: {} } }),
    { getState: () => ({ user: { id: "u-1" } }) },
  ),
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ workspaces: [], ready: true }),
}));

vi.mock("@multica/core/onboarding", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@multica/core/onboarding",
  );
  return {
    ...actual,
    completeOnboarding: (...args: unknown[]) => mockCompleteOnboarding(...args),
    createPersonalAgent: (...args: unknown[]) => mockCreatePersonalAgent(...args),
  };
});

const NEW_WORKSPACE = { id: "ws-new", slug: "new-ws", name: "New" };

vi.mock("./steps/step-workspace", () => ({
  StepWorkspace: ({ onCreated }: { onCreated: (ws: unknown) => void }) => (
    <button type="button" onClick={() => onCreated(NEW_WORKSPACE)}>
      create workspace
    </button>
  ),
}));

vi.mock("./steps/step-agent-setup", () => ({
  StepAgentSetup: ({
    onSkip,
    onCreate,
    userName,
  }: {
    onSkip: () => Promise<void>;
    onCreate: (request: unknown) => Promise<void>;
    userName: string;
  }) => (
    <>
      <button type="button" onClick={() => void onSkip()}>
        skip runtime
      </button>
      <button
        type="button"
        onClick={() =>
          void onCreate({
            runtime: { id: "rt-1" },
            name: `${userName}'s assistant`,
            description: "personal",
          }).catch(() => undefined)
        }
      >
        create agent
      </button>
    </>
  ),
}));

import { OnboardingFlow } from "./onboarding-flow";

function renderAtRuntimeStep() {
  const onComplete = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <OnboardingFlow
          mode="new_workspace"
          onCancel={vi.fn()}
          onComplete={onComplete}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "create workspace" }));
  return {
    onComplete,
    skip: screen.getByRole("button", { name: "skip runtime" }),
    create: screen.getByRole("button", { name: "create agent" }),
  };
}

function duplicateGuideConflict(body?: unknown) {
  return new ApiError(
    "Active duplicate issue exists",
    409,
    "Conflict",
    body ?? {
      code: "active_duplicate_issue",
      error: "An active issue with this title already exists",
      issue: {
        id: "issue-existing",
        identifier: "NEW-1",
        title: "Connect an AI coding tool and create your agent",
      },
    },
  );
}

beforeEach(() => {
  mockCreateIssue.mockReset();
  mockCompleteOnboarding.mockReset();
  mockToastError.mockReset();
  mockCreatePersonalAgent.mockReset();
});

describe("OnboardingFlow — creating the personal agent", () => {
  it("creates the agent in the new workspace, completes onboarding, then opens its chat", async () => {
    mockCreatePersonalAgent.mockResolvedValueOnce({
      agent: { id: "agent-1" },
      chatSession: { id: "session-1" },
    });
    mockCompleteOnboarding.mockResolvedValueOnce(undefined);
    const { onComplete, create } = renderAtRuntimeStep();

    fireEvent.click(create);

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, { kind: "chat", sessionId: "session-1" });
    });
    expect(mockCreatePersonalAgent).toHaveBeenCalledWith({
      workspaceSlug: "new-ws",
      workspaceId: "ws-new",
      queryClient: expect.any(QueryClient),
      runtimeId: "rt-1",
      name: "Ada's assistant",
      description: "personal",
    });
    expect(mockCompleteOnboarding).toHaveBeenCalledWith("full", "ws-new");
    expect(mockCreatePersonalAgent.mock.invocationCallOrder[0]).toBeLessThan(
      mockCompleteOnboarding.mock.invocationCallOrder[0]!,
    );
  });

  it("does not complete onboarding when the agent cannot be created", async () => {
    mockCreatePersonalAgent.mockRejectedValueOnce(new Error("runtime offline"));
    const { onComplete, create } = renderAtRuntimeStep();

    fireEvent.click(create);

    await waitFor(() => expect(mockCreatePersonalAgent).toHaveBeenCalledOnce());
    expect(mockCompleteOnboarding).not.toHaveBeenCalled();
    expect(onComplete).not.toHaveBeenCalled();
  });
});

describe("OnboardingFlow — skipping runtime setup", () => {
  it("creates the guide issue in the new workspace, then opens it", async () => {
    mockCreateIssue.mockResolvedValueOnce({ id: "issue-guide", identifier: "NEW-2" });
    mockCompleteOnboarding.mockResolvedValueOnce(undefined);
    const { onComplete, skip } = renderAtRuntimeStep();

    fireEvent.click(skip);

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, {
        kind: "issue",
        issueRef: "NEW-2",
      });
    });
    const [body, workspaceSlug] = mockCreateIssue.mock.calls[0]!;
    expect(workspaceSlug).toBe("new-ws");
    expect(body).toMatchObject({
      title: "Connect an AI coding tool and create your agent",
      status: "in_progress",
      assignee_type: "member",
      assignee_id: "u-1",
    });
    expect(mockCompleteOnboarding).toHaveBeenCalledWith(
      "runtime_skipped",
      "ws-new",
    );
    // Completing onboarding refreshes the user, and the web shell unmounts the
    // flow as soon as it is marked onboarded — so the guide must exist first.
    expect(mockCreateIssue.mock.invocationCallOrder[0]).toBeLessThan(
      mockCompleteOnboarding.mock.invocationCallOrder[0]!,
    );
  });

  it("stays on the step when the guide cannot be created", async () => {
    mockCreateIssue
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce({ id: "issue-guide", identifier: "NEW-2" });
    mockCompleteOnboarding.mockResolvedValue(undefined);
    const { onComplete, skip } = renderAtRuntimeStep();

    fireEvent.click(skip);
    await waitFor(() => expect(mockToastError).toHaveBeenCalledOnce());
    expect(mockCompleteOnboarding).not.toHaveBeenCalled();
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(skip);
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, {
        kind: "issue",
        issueRef: "NEW-2",
      });
    });
  });

  it("reuses the guide issue when only completing onboarding failed", async () => {
    mockCreateIssue.mockResolvedValueOnce({ id: "issue-guide", identifier: "NEW-2" });
    mockCompleteOnboarding
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce(undefined);
    const { onComplete, skip } = renderAtRuntimeStep();

    fireEvent.click(skip);
    await waitFor(() => expect(mockToastError).toHaveBeenCalledOnce());
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(skip);
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, {
        kind: "issue",
        issueRef: "NEW-2",
      });
    });
    expect(mockCreateIssue).toHaveBeenCalledTimes(1);
  });

  // A reload drops the in-memory guide id while onboarding is still
  // incomplete, so the next Skip hits the server's duplicate guard.
  it("opens the existing guide after a reload between creating it and completing onboarding", async () => {
    mockCreateIssue
      .mockResolvedValueOnce({ id: "issue-existing", identifier: "NEW-1" })
      .mockRejectedValueOnce(duplicateGuideConflict());
    mockCompleteOnboarding
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce(undefined);

    const first = renderAtRuntimeStep();
    fireEvent.click(first.skip);
    await waitFor(() => expect(mockToastError).toHaveBeenCalledOnce());
    cleanup();

    const reloaded = renderAtRuntimeStep();
    fireEvent.click(reloaded.skip);
    await waitFor(() => {
      expect(reloaded.onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, {
        kind: "issue",
        issueRef: "NEW-1",
      });
    });
    expect(mockCompleteOnboarding).toHaveBeenCalledTimes(2);
    expect(mockToastError).toHaveBeenCalledOnce();
  });

  it("opens the existing guide when the create response was lost", async () => {
    mockCreateIssue
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockRejectedValueOnce(duplicateGuideConflict());
    mockCompleteOnboarding.mockResolvedValue(undefined);
    const { onComplete, skip } = renderAtRuntimeStep();

    fireEvent.click(skip);
    await waitFor(() => expect(mockToastError).toHaveBeenCalledOnce());
    expect(mockCompleteOnboarding).not.toHaveBeenCalled();

    fireEvent.click(skip);
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, {
        kind: "issue",
        issueRef: "NEW-1",
      });
    });
  });

  it("does not treat a 409 without a valid duplicate body as the guide", async () => {
    mockCreateIssue.mockRejectedValueOnce(
      duplicateGuideConflict({ code: "active_duplicate_issue" }),
    );
    const { onComplete, skip } = renderAtRuntimeStep();

    fireEvent.click(skip);

    await waitFor(() => expect(mockToastError).toHaveBeenCalledOnce());
    expect(mockCompleteOnboarding).not.toHaveBeenCalled();
    expect(onComplete).not.toHaveBeenCalled();
  });
});

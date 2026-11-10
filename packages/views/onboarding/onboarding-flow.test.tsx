import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { getTestResources } from "../test/i18n";

const m = vi.hoisted(() => ({
  listWorkspaces: vi.fn(),
  createWorkspace: vi.fn(),
  listAgents: vi.fn(),
  createAgent: vi.fn(),
  createIssue: vi.fn(),
  createProject: vi.fn(),
  completeOnboarding: vi.fn(),
  activate: vi.fn(),
  toastError: vi.fn(),
  creationDisabled: false,
}));

vi.mock("sonner", () => ({ toast: { error: m.toastError, success: vi.fn() } }));
vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<Record<string, unknown>>("@multica/core/api");
  return {
    ...actual,
    api: {
      getBaseUrl: () => "https://multica.ai",
      listWorkspaces: m.listWorkspaces,
      listAgents: m.listAgents,
      createAgent: m.createAgent,
      createIssue: m.createIssue,
      createProject: m.createProject,
    },
  };
});

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: Record<string, unknown>) => unknown) =>
    selector({ workspaceCreationDisabled: m.creationDisabled, daemonAppUrl: "" }),
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutateAsync: m.createWorkspace, mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/auth", () => {
  const user = { id: "u-1", name: "Ada Lovelace", email: "ada@example.com" };
  return {
    useAuthStore: Object.assign(
      (selector: (s: { user: unknown }) => unknown) => selector({ user }),
      { getState: () => ({ user }) },
    ),
  };
});

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ workspaces: [], ready: true }),
}));

vi.mock("@multica/core/onboarding", async () => {
  const actual = await vi.importActual<Record<string, unknown>>("@multica/core/onboarding");
  return {
    ...actual,
    completeOnboarding: m.completeOnboarding,
    useGettingStartedStore: (selector: (s: { activate: unknown }) => unknown) =>
      selector({ activate: m.activate }),
  };
});

const RUNTIME = {
  id: "rt-1",
  provider: "claude",
  status: "online",
  daemon_id: "daemon-1",
  device_info: "MacBook",
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
};

vi.mock("./components/use-onboarding-runtimes", () => ({
  useOnboardingRuntimes: () => ({
    runtimes: [RUNTIME],
    detecting: false,
    canRescan: false,
    rescan: vi.fn(),
  }),
}));

// The picker's own rendering is covered elsewhere; here it only has to hand
// the flow a selection.
vi.mock("./components/tool-picker", () => ({
  EMPTY_TOOL_SELECTION: { runtimeId: "", model: "", thinkingLevel: "" },
  ToolPicker: ({ onChange }: { onChange: (v: unknown) => void }) => (
    <button
      type="button"
      onClick={() => onChange({ runtimeId: "rt-1", model: "opus", thinkingLevel: "" })}
    >
      pick claude
    </button>
  ),
}));

vi.mock("./steps/step-workspace", () => ({
  StepWorkspace: ({ onCreated }: { onCreated: (ws: unknown) => void }) => (
    <button type="button" onClick={() => onCreated(NEW_WORKSPACE)}>
      create workspace
    </button>
  ),
}));

import { OnboardingFlow } from "./onboarding-flow";

const NEW_WORKSPACE = { id: "ws-new", slug: "ada", name: "Ada's workspace" };

function renderFlow(props: Partial<Parameters<typeof OnboardingFlow>[0]> = {}) {
  const onComplete = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={getTestResources("en")}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    );
  }
  render(<OnboardingFlow onComplete={onComplete} {...props} />, { wrapper: Wrapper });
  return { onComplete };
}

async function startAndPickTool() {
  fireEvent.click(screen.getByRole("button", { name: "Start" }));
  fireEvent.click(await screen.findByRole("button", { name: "pick claude" }));
  fireEvent.click(screen.getByRole("button", { name: "Continue with Claude Code" }));
}

beforeEach(() => {
  for (const fn of Object.values(m)) if (typeof fn === "function") fn.mockReset();
  m.creationDisabled = false;
  m.listWorkspaces.mockResolvedValue([]);
  m.createWorkspace.mockResolvedValue(NEW_WORKSPACE);
  m.listAgents.mockResolvedValue([]);
  m.createAgent.mockResolvedValue({ id: "agent-1", name: "Claude Code" });
  m.createIssue.mockResolvedValue({ id: "issue-1" });
  m.completeOnboarding.mockResolvedValue(undefined);
});

describe("OnboardingFlow — first run", () => {
  it("creates the member's workspace for them on Start", async () => {
    renderFlow();

    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await screen.findByRole("heading", { name: "Which coding tool should your agent use?" });
    expect(m.createWorkspace).toHaveBeenCalledWith({ name: "Ada's workspace", slug: "ada-lovelace" });
  });

  it("retries a taken slug with a suffix instead of asking", async () => {
    m.createWorkspace
      .mockRejectedValueOnce(new ApiError("taken", 409, "Conflict"))
      .mockResolvedValueOnce(NEW_WORKSPACE);
    renderFlow();

    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await screen.findByRole("heading", { name: "Which coding tool should your agent use?" });
    expect(m.createWorkspace).toHaveBeenCalledTimes(2);
    expect(m.createWorkspace.mock.calls[1]![0].slug).toMatch(/^ada-lovelace-[a-z0-9]{4}$/);
  });

  it("continues in a workspace left by an earlier, unfinished run", async () => {
    m.listWorkspaces.mockResolvedValue([NEW_WORKSPACE]);
    renderFlow();

    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await screen.findByRole("button", { name: "pick claude" });
    expect(m.createWorkspace).not.toHaveBeenCalled();
  });

  it("falls back to naming the workspace when creation is refused", async () => {
    m.createWorkspace.mockRejectedValueOnce(new ApiError("forbidden", 403, "Forbidden"));
    renderFlow();

    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    expect(await screen.findByRole("button", { name: "create workspace" })).toBeInTheDocument();
  });

  it("creates the agent and the task, then completes onboarding and opens the task", async () => {
    const { onComplete } = renderFlow();
    await startAndPickTool();

    fireEvent.change(await screen.findByRole("textbox"), {
      target: { value: "Summarize the open bugs" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Assign to Claude Code" }));

    await waitFor(() =>
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, { kind: "issue", issueId: "issue-1" }),
    );
    const [agentBody, agentSlug] = m.createAgent.mock.calls[0]!;
    expect(agentSlug).toBe("ada");
    expect(agentBody).toMatchObject({ name: "Claude Code", runtime_id: "rt-1", model: "opus" });
    expect(m.createIssue.mock.calls[0]).toEqual([
      {
        title: "Summarize the open bugs",
        status: "todo",
        assignee_type: "agent",
        assignee_id: "agent-1",
      },
      "ada",
    ]);
    expect(m.activate).toHaveBeenCalledWith("ws-new");
    expect(m.completeOnboarding).toHaveBeenCalledWith("full", "ws-new");
    // The web shell unmounts the flow the moment onboarding completes, so
    // every write has to land first.
    expect(m.createIssue.mock.invocationCallOrder[0]).toBeLessThan(
      m.completeOnboarding.mock.invocationCallOrder[0]!,
    );
  });

  it("does not file a second task when only completing onboarding failed", async () => {
    m.completeOnboarding
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce(undefined);
    const { onComplete } = renderFlow();
    await startAndPickTool();
    fireEvent.change(await screen.findByRole("textbox"), { target: { value: "Do it" } });
    const submit = screen.getByRole("button", { name: "Assign to Claude Code" });

    fireEvent.click(submit);
    await waitFor(() => expect(m.toastError).toHaveBeenCalledOnce());
    fireEvent.click(submit);

    await waitFor(() =>
      expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, { kind: "issue", issueId: "issue-1" }),
    );
    expect(m.createIssue).toHaveBeenCalledTimes(1);
  });

  it("skipping the tool finishes without an agent and lands on the workspace", async () => {
    const { onComplete } = renderFlow();
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    fireEvent.click(await screen.findByRole("button", { name: "Skip for now" }));

    await waitFor(() => expect(onComplete).toHaveBeenCalledWith(NEW_WORKSPACE, undefined));
    expect(m.completeOnboarding).toHaveBeenCalledWith("runtime_skipped", "ws-new");
    expect(m.createAgent).not.toHaveBeenCalled();
  });
});

describe("OnboardingFlow — new-workspace mode", () => {
  it("starts at naming the workspace, then goes to the tool", async () => {
    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });

    expect(screen.queryByRole("button", { name: "Start" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "create workspace" }));

    expect(await screen.findByRole("button", { name: "pick claude" })).toBeInTheDocument();
    expect(m.createWorkspace).not.toHaveBeenCalled();
  });
});

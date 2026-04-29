import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime, AgentTask } from "@multica/core/types";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const { mockListTasksByIssue, mockListRuntimes, toastSuccess, toastError } =
  vi.hoisted(() => ({
    mockListTasksByIssue: vi.fn<() => Promise<AgentTask[]>>(),
    mockListRuntimes: vi.fn<() => Promise<AgentRuntime[]>>(),
    toastSuccess: vi.fn(),
    toastError: vi.fn(),
  }));

vi.mock("@multica/core/api", () => ({
  api: {
    listTasksByIssue: () => mockListTasksByIssue(),
    listRuntimes: () => mockListRuntimes(),
  },
}));

vi.mock("@multica/core/runtimes/queries", async () => {
  const actual = await vi.importActual<
    typeof import("@multica/core/runtimes/queries")
  >("@multica/core/runtimes/queries");
  return {
    ...actual,
    runtimeListOptions: () => ({
      queryKey: ["runtimes", "ws-1", "list"],
      queryFn: () => mockListRuntimes(),
    }),
  };
});

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError },
}));

import { useTakeOver } from "../use-take-over";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "t-1",
    agent_id: "a-1",
    runtime_id: "rt-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: "2026-04-29T12:00:00Z",
    result: { session_id: "sess-1", work_dir: "/tmp/work" },
    error: null,
    created_at: "2026-04-29T11:55:00Z",
    ...overrides,
  };
}

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "claude-runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const takeOverIssueIPC = vi.fn();

beforeEach(() => {
  mockListTasksByIssue.mockReset();
  mockListRuntimes.mockReset();
  toastSuccess.mockReset();
  toastError.mockReset();
  takeOverIssueIPC.mockReset();
  mockListTasksByIssue.mockResolvedValue([makeTask()]);
  mockListRuntimes.mockResolvedValue([makeRuntime()]);
  (window as unknown as { desktopAPI?: unknown }).desktopAPI = {
    takeOverIssue: takeOverIssueIPC,
  };
});

afterEach(() => {
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

describe("useTakeOver", () => {
  it("is invisible when running on the web (no desktopAPI)", async () => {
    delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;

    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );

    // Without desktopAPI we should not even fire the queries.
    await waitFor(() => {
      expect(result.current.visible).toBe(false);
    });
    expect(mockListTasksByIssue).not.toHaveBeenCalled();
    expect(mockListRuntimes).not.toHaveBeenCalled();
  });

  it("is invisible when disabled, even on desktop", async () => {
    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: false }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.visible).toBe(false);
    });
    expect(mockListTasksByIssue).not.toHaveBeenCalled();
  });

  it("becomes visible on desktop when a resumable run exists", async () => {
    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.visible).toBe(true);
    });
  });

  it("stays invisible when no completed run carries a session id", async () => {
    mockListTasksByIssue.mockResolvedValue([makeTask({ result: {} })]);

    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );

    // Wait for the queries to settle.
    await waitFor(() => {
      expect(mockListTasksByIssue).toHaveBeenCalled();
    });
    expect(result.current.visible).toBe(false);
  });

  it("stays invisible when the runtime provider isn't supported", async () => {
    mockListRuntimes.mockResolvedValue([makeRuntime({ provider: "openai" })]);

    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );

    await waitFor(() => {
      expect(mockListRuntimes).toHaveBeenCalled();
    });
    expect(result.current.visible).toBe(false);
  });

  it("invokes the desktop IPC and shows a success toast with the workdir", async () => {
    takeOverIssueIPC.mockResolvedValue({
      ok: true,
      command: "cd '/Users/x/work' && claude --resume sess-1",
      workDir: "/Users/x/work",
    });

    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );
    await waitFor(() => {
      expect(result.current.visible).toBe(true);
    });

    await act(async () => {
      await result.current.takeOver();
    });

    expect(takeOverIssueIPC).toHaveBeenCalledWith("issue-1", "ws-1");
    expect(toastSuccess).toHaveBeenCalledWith(
      "Command copied. Paste in your terminal to take over.",
      { description: "/Users/x/work" },
    );
    expect(toastError).not.toHaveBeenCalled();
  });

  it("shows an error toast when the IPC call fails", async () => {
    takeOverIssueIPC.mockResolvedValue({
      ok: false,
      error: "no completed run found for issue issue-1",
    });

    const { result } = renderHook(
      () => useTakeOver("issue-1", { enabled: true }),
      { wrapper },
    );
    await waitFor(() => {
      expect(result.current.visible).toBe(true);
    });

    await act(async () => {
      await result.current.takeOver();
    });

    expect(toastError).toHaveBeenCalledWith(
      "Could not prepare take-over command",
      { description: "no completed run found for issue issue-1" },
    );
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});

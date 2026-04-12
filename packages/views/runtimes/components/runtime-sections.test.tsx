import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError, api } from "@multica/core/api";
import {
  useDaemonUpdateStore,
  useRuntimePingStore,
} from "@multica/core/runtimes";
import { setCurrentWorkspaceId } from "@multica/core/platform";
import { PingSection } from "./ping-section";
import { UpdateSection } from "./update-section";

vi.mock("@multica/core/api", () => {
  class MockApiError extends Error {
    status: number;

    constructor(message: string, status: number) {
      super(message);
      this.name = "ApiError";
      this.status = status;
    }
  }

  return {
    ApiError: MockApiError,
    api: {
      pingRuntime: vi.fn(),
      getPingResult: vi.fn(),
      initiateUpdate: vi.fn(),
      getUpdateResult: vi.fn(),
    },
  };
});

function renderWithClient(node: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>{node}</QueryClientProvider>,
  );
}

describe("PingSection", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-04-12T10:00:00Z"));
    localStorage.clear();
    setCurrentWorkspaceId("ws-1");
    useRuntimePingStore.persist.clearStorage();
    useRuntimePingStore.setState({ entries: {} });
    vi.mocked(api.pingRuntime).mockReset();
    vi.mocked(api.getPingResult).mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("restores a started ping after remount", async () => {
    vi.mocked(api.pingRuntime).mockResolvedValue({
      id: "ping-1",
      runtime_id: "runtime-1",
      status: "pending",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });
    vi.mocked(api.getPingResult)
      .mockResolvedValueOnce({
        id: "ping-1",
        runtime_id: "runtime-1",
        status: "running",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      })
      .mockResolvedValueOnce({
        id: "ping-1",
        runtime_id: "runtime-1",
        status: "completed",
        output: "pong",
        duration_ms: 120,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTimeAsync });
    const firstRender = renderWithClient(<PingSection runtimeId="runtime-1" />);

    await user.click(screen.getByRole("button", { name: /test connection/i }));
    await vi.advanceTimersByTimeAsync(2_000);

    expect(useRuntimePingStore.getState().entries["runtime-1"]?.requestId).toBe(
      "ping-1",
    );

    firstRender.unmount();
    renderWithClient(<PingSection runtimeId="runtime-1" />);

    await vi.advanceTimersByTimeAsync(2_000);

    await waitFor(() => {
      expect(screen.getByText("Connected")).toBeInTheDocument();
    });
    expect(screen.getByText("pong")).toBeInTheDocument();
  });

  it("clears expired ping state when the server returns 404", async () => {
    useRuntimePingStore.getState().setEntry({
      runtimeId: "runtime-404",
      requestId: "ping-404",
      status: "running",
      startedAt: Date.now(),
    });
    vi.mocked(api.getPingResult).mockRejectedValue(
      new ApiError("ping not found", 404),
    );

    renderWithClient(<PingSection runtimeId="runtime-404" />);
    await vi.advanceTimersByTimeAsync(2_000);

    await waitFor(() => {
      expect(screen.getByText("Previous test status expired")).toBeInTheDocument();
    });
    expect(useRuntimePingStore.getState().entries["runtime-404"]).toBeUndefined();
  });

  it("marks ping polling as interrupted after repeated non-404 failures", async () => {
    useRuntimePingStore.getState().setEntry({
      runtimeId: "runtime-fail",
      requestId: "ping-fail",
      status: "running",
      startedAt: Date.now(),
    });
    vi.mocked(api.getPingResult).mockRejectedValue(new Error("network"));

    renderWithClient(<PingSection runtimeId="runtime-fail" />);
    await vi.advanceTimersByTimeAsync(10_000);

    await waitFor(() => {
      expect(useRuntimePingStore.getState().entries["runtime-fail"]?.status).toBe(
        "interrupted",
      );
    });
  });
});

describe("UpdateSection", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-04-12T10:00:00Z"));
    localStorage.clear();
    useDaemonUpdateStore.persist.clearStorage();
    useDaemonUpdateStore.setState({ entries: {} });
    vi.mocked(api.initiateUpdate).mockReset();
    vi.mocked(api.getUpdateResult).mockReset();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: "v1.2.0" }),
      }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("shows shared update state for another runtime on the same daemon", async () => {
    useDaemonUpdateStore.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.0",
      status: "running",
      startedAt: Date.now(),
    });
    vi.mocked(api.getUpdateResult).mockResolvedValue({
      id: "update-1",
      runtime_id: "runtime-a",
      status: "running",
      target_version: "v1.2.0",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });

    renderWithClient(
      <UpdateSection
        runtimeId="runtime-b"
        daemonId="daemon-1"
        currentVersion="v1.0.0"
        isOnline
      />,
    );

    expect(await screen.findByText("Updating...")).toBeInTheDocument();
    await vi.advanceTimersByTimeAsync(2_000);

    expect(api.getUpdateResult).toHaveBeenCalledWith("runtime-a", "update-1");
  });

  it("handles 409 conflicts with a transient notice instead of a failure state", async () => {
    vi.mocked(api.initiateUpdate).mockRejectedValue(
      new ApiError("already running", 409),
    );

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTimeAsync });
    renderWithClient(
      <UpdateSection
        runtimeId="runtime-a"
        daemonId="daemon-1"
        currentVersion="v1.0.0"
        isOnline
      />,
    );

    await user.click(await screen.findByRole("button", { name: /update/i }));

    expect(
      screen.getByText("An update is already in progress, please wait"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Update failed")).not.toBeInTheDocument();
    expect(useDaemonUpdateStore.getState().entries["daemon-1"]).toBeUndefined();
  });

  it("clears completed shared state once the current version catches up", async () => {
    useDaemonUpdateStore.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.0",
      status: "completed",
      startedAt: Date.now() - 5_000,
      finishedAt: Date.now() - 1_000,
      output: "Updated to v1.2.0",
    });

    renderWithClient(
      <UpdateSection
        runtimeId="runtime-b"
        daemonId="daemon-1"
        currentVersion="v1.2.0"
        isOnline
      />,
    );

    await waitFor(() => {
      expect(useDaemonUpdateStore.getState().entries["daemon-1"]).toBeUndefined();
    });
  });

  it("keeps failed update state until the next attempt replaces it", async () => {
    useDaemonUpdateStore.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.0",
      status: "failed",
      startedAt: Date.now() - 5_000,
      finishedAt: Date.now() - 1_000,
      error: "boom",
    });
    vi.mocked(api.initiateUpdate).mockResolvedValue({
      id: "update-2",
      runtime_id: "runtime-b",
      status: "pending",
      target_version: "v1.2.0",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTimeAsync });
    renderWithClient(
      <UpdateSection
        runtimeId="runtime-b"
        daemonId="daemon-1"
        currentVersion="v1.0.0"
        isOnline
      />,
    );

    expect(await screen.findByText("boom")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /retry/i }));

    expect(useDaemonUpdateStore.getState().entries["daemon-1"]).toMatchObject({
      requestId: "update-2",
      runtimeId: "runtime-b",
      status: "pending",
    });
  });

  it("allows dismissing shared update errors without starting a new attempt", async () => {
    useDaemonUpdateStore.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.0",
      status: "failed",
      startedAt: Date.now() - 5_000,
      finishedAt: Date.now() - 1_000,
      error: "boom",
    });

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTimeAsync });
    renderWithClient(
      <UpdateSection
        runtimeId="runtime-b"
        daemonId="daemon-1"
        currentVersion="v1.0.0"
        isOnline
      />,
    );

    expect(await screen.findByText("boom")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /dismiss/i }));

    await waitFor(() => {
      expect(useDaemonUpdateStore.getState().entries["daemon-1"]).toBeUndefined();
    });
    expect(screen.queryByText("boom")).not.toBeInTheDocument();
    expect(api.initiateUpdate).not.toHaveBeenCalled();
  });

  it("keeps interrupted state through refresh and resumes polling within the recovery window", async () => {
    useDaemonUpdateStore.getState().setEntry({
      daemonId: "daemon-1",
      runtimeId: "runtime-a",
      requestId: "update-1",
      targetVersion: "v1.2.0",
      status: "interrupted",
      startedAt: Date.now() - 60_000,
      error: "connection lost",
    });
    vi.mocked(api.getUpdateResult).mockResolvedValue({
      id: "update-1",
      runtime_id: "runtime-a",
      status: "running",
      target_version: "v1.2.0",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });

    renderWithClient(
      <UpdateSection
        runtimeId="runtime-b"
        daemonId="daemon-1"
        currentVersion="v1.0.0"
        isOnline
      />,
    );

    await vi.advanceTimersByTimeAsync(2_000);

    expect(api.getUpdateResult).toHaveBeenCalledWith("runtime-a", "update-1");
    await waitFor(() => {
      expect(useDaemonUpdateStore.getState().entries["daemon-1"]?.status).toBe(
        "running",
      );
    });
  });
});

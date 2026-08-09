// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

const migrateSpy = vi.hoisted(() => vi.fn());
const mergeEnvSpy = vi.hoisted(() => vi.fn());

vi.mock("../api", () => ({
  api: { migrateAgentsToRuntime: migrateSpy, mergeAgentsEnv: mergeEnvSpy },
}));

import { useMigrateAgentsToRuntime } from "./mutations";
import { useMergeAgentsEnv } from "../agents/use-merge-agents-env";

const WS = "ws-1";

function wrapper(qc: QueryClient) {
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

beforeEach(() => {
  migrateSpy.mockReset();
  migrateSpy.mockResolvedValue({
    target_runtime_id: "rt-b",
    dry_run: false,
    migrated: [],
    skipped: [],
    tasks_migrated: 2,
    tasks_staying_active: 0,
  });
  mergeEnvSpy.mockReset();
  mergeEnvSpy.mockResolvedValue({ results: [], skipped: [] });
});

describe("useMigrateAgentsToRuntime", () => {
  it("refreshes runtimes, agents AND the task snapshot after a migration", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const spy = vi.spyOn(qc, "invalidateQueries");
    const { result } = renderHook(() => useMigrateAgentsToRuntime(WS), {
      wrapper: wrapper(qc),
    });

    await result.current.mutateAsync({
      targetRuntimeId: "rt-b",
      agentIds: ["agent-1"],
    });
    await waitFor(() => expect(spy).toHaveBeenCalled());

    const keys = spy.mock.calls.map((c) =>
      JSON.stringify((c[0] as { queryKey: unknown[] }).queryKey),
    );
    // Three projections move together. The task snapshot is the one that is
    // easy to forget and load-bearing: presence is derived from it, so without
    // this the list keeps attributing migrated work to the old runtime.
    expect(keys.some((k) => k.includes("runtimes"))).toBe(true);
    expect(keys.some((k) => k.includes("agents"))).toBe(true);
    expect(keys.some((k) => k.includes("agent-task-snapshot"))).toBe(true);
  });

  it("passes the stale-plan guard through to the API when the caller sets it", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(() => useMigrateAgentsToRuntime(WS), {
      wrapper: wrapper(qc),
    });

    await result.current.mutateAsync({
      targetRuntimeId: "rt-b",
      agentIds: ["agent-1"],
      expectedSourceRuntimeId: "rt-a",
    });

    expect(migrateSpy).toHaveBeenCalledWith("rt-b", {
      agent_ids: ["agent-1"],
      expected_source_runtime_id: "rt-a",
      clear_model_settings: undefined,
    });
  });

  it("passes the optional replacement model settings through in wire casing", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(() => useMigrateAgentsToRuntime(WS), {
      wrapper: wrapper(qc),
    });

    await result.current.mutateAsync({
      targetRuntimeId: "rt-b",
      agentIds: ["agent-1"],
      model: "claude-sonnet-5",
      thinkingLevel: "high",
      serviceTier: "priority",
    });

    expect(migrateSpy).toHaveBeenCalledWith("rt-b", {
      agent_ids: ["agent-1"],
      expected_source_runtime_id: undefined,
      clear_model_settings: undefined,
      model: "claude-sonnet-5",
      thinking_level: "high",
      service_tier: "priority",
    });
  });

  it("still refreshes the caches when the migration fails", async () => {
    // onSettled, not onSuccess: a failed request may still have been a partial
    // observation of a changed world (409 carries a fresh agent set), and a
    // stale list after an error is worse than one extra refetch.
    migrateSpy.mockRejectedValue(new Error("plan changed"));
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const spy = vi.spyOn(qc, "invalidateQueries");
    const { result } = renderHook(() => useMigrateAgentsToRuntime(WS), {
      wrapper: wrapper(qc),
    });

    await expect(
      result.current.mutateAsync({
        targetRuntimeId: "rt-b",
        agentIds: ["agent-1"],
      }),
    ).rejects.toThrow();
    await waitFor(() => expect(spy).toHaveBeenCalled());
  });
});

describe("useMergeAgentsEnv", () => {
  it("refreshes the agent list so the configured-variable count updates", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const spy = vi.spyOn(qc, "invalidateQueries");
    const { result } = renderHook(() => useMergeAgentsEnv(WS), {
      wrapper: wrapper(qc),
    });

    await result.current.mutateAsync({
      agentIds: ["agent-1"],
      set: { KEY: "value" },
    });
    await waitFor(() => expect(spy).toHaveBeenCalled());

    const keys = spy.mock.calls.map((c) =>
      JSON.stringify((c[0] as { queryKey: unknown[] }).queryKey),
    );
    expect(keys.some((k) => k.includes("agents"))).toBe(true);
    // Env injection touches neither runtimes nor tasks, so it must not
    // invalidate them.
    expect(keys.some((k) => k.includes("agent-task-snapshot"))).toBe(false);
  });
});

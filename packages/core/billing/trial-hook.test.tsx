// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { WorkspaceSubscriptionSummary } from "../types";
import { useWorkspaceTrial } from "./trial";
import { usePaidFeatureAccess, workspaceFeatureKeys } from "./paid-features";
import { workspaceSubscriptionKeys } from "./workspace-subscription-queries";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// The server may cache the trial's Business policy for up to a minute past the
// end; the hook refetches again once that window has passed.
it("refreshes feature access after the last Business policy expires on the server", async () => {
  vi.useFakeTimers();
  const endsAt = Date.parse("2030-01-15T00:00:00Z");
  vi.setSystemTime(endsAt - 1000);
  const active = {
    entitlement: { plan: "business", status: "trialing" },
    trial: {
      plan: "business", state: "active",
      startedAt: "2030-01-01T00:00:00Z", endsAt: new Date(endsAt).toISOString(),
    },
  } as WorkspaceSubscriptionSummary;
  // A policy read just before trial expiry is fresh for another minute.
  const serverPolicyFreshUntil = endsAt + 59_000;
  const getWorkspaceFeatures = vi.fn(async () => ({
    features: { private_projects: Date.now() < serverPolicyFreshUntil ? "available" : "locked" },
  }));
  const getWorkspaceSubscriptionSummary = vi.fn(async () => Date.now() < endsAt ? active : ({
    ...active, entitlement: { plan: "free", status: "inactive" },
    trial: { ...active.trial, state: "expired" },
  }));
  setApiInstance({ getWorkspaceFeatures, getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(workspaceSubscriptionKeys.summary("ws-trial"), active);
  qc.setQueryData(workspaceFeatureKeys.all("ws-trial"), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => ({
    trial: useWorkspaceTrial("ws-trial", true),
    access: usePaidFeatureAccess("ws-trial", "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.trial.kind).toBe("ended");
  expect(getWorkspaceFeatures).toHaveBeenCalledTimes(1);
  await act(async () => { await vi.advanceTimersByTimeAsync(5 * 60 * 1000); });
  expect(result.current.access.state).toBe("read_only");
  qc.clear();
});

function trialSummary(state: "active" | "expired", endsAt: number): WorkspaceSubscriptionSummary {
  return {
    entitlement: state === "active"
      ? { plan: "business", status: "trialing" }
      : { plan: "free", status: "inactive" },
    trial: {
      plan: "business",
      state,
      startedAt: new Date(endsAt - 14 * 24 * 60 * 60 * 1000).toISOString(),
      endsAt: new Date(endsAt).toISOString(),
    },
  } as WorkspaceSubscriptionSummary;
}

// A desktop window switches workspaces without remounting the shell. The
// workspace switched to has to be read at the current time, and an end that
// passed while it was not showing has to be caught up.
it("reads a workspace switched to at the current time and catches up its missed end", async () => {
  vi.useFakeTimers();
  const start = Date.parse("2030-01-01T00:00:00Z");
  vi.setSystemTime(start);
  const otherEndsAt = start + 10 * 24 * 60 * 60 * 1000;
  const endedAt = start + 10 * 60 * 1000;
  const getWorkspaceSubscriptionSummary = vi.fn(async () => trialSummary("expired", endedAt));
  const getWorkspaceFeatures = vi.fn(async () => ({ features: { private_projects: "locked" } }));
  setApiInstance({ getWorkspaceFeatures, getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(workspaceSubscriptionKeys.summary("ws-a"), trialSummary("active", otherEndsAt));
  // Read while ws-b's trial was still running.
  qc.setQueryData(workspaceSubscriptionKeys.summary("ws-b"), trialSummary("active", endedAt));
  qc.setQueryData(workspaceFeatureKeys.all("ws-b"), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, rerender } = renderHook(
    ({ wsId }) => ({
      trial: useWorkspaceTrial(wsId, true),
      access: usePaidFeatureAccess(wsId, "private_projects", { hasExistingConfig: true }),
    }),
    { wrapper, initialProps: { wsId: "ws-a" } },
  );
  expect(result.current.trial.kind).toBe("active");

  // Half an hour later, short of the hourly tick: ws-b ended 20 minutes ago.
  vi.setSystemTime(start + 30 * 60 * 1000);
  rerender({ wsId: "ws-b" });
  await act(async () => { await vi.advanceTimersByTimeAsync(100); });
  expect(result.current.trial).toMatchObject({ kind: "ended", confirmed: true, settled: false });
  // Paid-feature reads settle a moment after Cloud's confirmation arrives.
  await act(async () => { await vi.advanceTimersByTimeAsync(6_000); });

  expect(result.current.trial).toMatchObject({ kind: "ended", settled: true });
  expect(getWorkspaceSubscriptionSummary).toHaveBeenCalled();
  expect(getWorkspaceFeatures).toHaveBeenCalled();
  expect(result.current.access.state).toBe("read_only");
  qc.clear();
});

it("does not refetch reads made after the end when a settled workspace shows again", async () => {
  vi.useFakeTimers();
  const endedAt = Date.parse("2030-01-01T00:00:00Z");
  vi.setSystemTime(endedAt + 60 * 60 * 1000);
  const getWorkspaceSubscriptionSummary = vi.fn(async () => trialSummary("expired", endedAt));
  setApiInstance({ getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(workspaceSubscriptionKeys.summary("ws-c"), trialSummary("expired", endedAt));
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => useWorkspaceTrial("ws-c", true), { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(6_000); });

  expect(result.current).toMatchObject({ kind: "ended", settled: true });
  expect(getWorkspaceSubscriptionSummary).not.toHaveBeenCalled();
  qc.clear();
});

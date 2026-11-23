// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { focusManager, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import { usePaidFeatureAccess, workspaceFeatureKeys } from "./paid-features";
import { useWorkspaceTrial } from "./trial";
import { workspaceSubscriptionKeys } from "./workspace-subscription-queries";

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
  vi.useRealTimers();
});

const endsAt = Date.parse("2030-01-15T00:00:00Z");
const trial = {
  plan: "business",
  startedAt: "2030-01-01T00:00:00Z",
  endsAt: new Date(endsAt).toISOString(),
};
const active = { entitlement: { plan: "business", status: "trialing" }, trial: { ...trial, state: "active" } };
const expired = { entitlement: { plan: "free", status: "inactive" }, trial: { ...trial, state: "expired" } };
const paid = { entitlement: { plan: "pro", status: "active" }, trial: null };
const ws = "ws-trial-recovery";

async function advance(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}

it("updates an open project's access when a refocus discovers a mid-trial Pro conversion", async () => {
  vi.useFakeTimers();
  const daySeven = endsAt - 7 * 24 * 60 * 60 * 1000;
  const returnAt = daySeven + 6 * 60 * 1000;
  vi.setSystemTime(daySeven);
  const getWorkspaceFeatures = vi.fn(async () => ({
    features: { private_projects: Date.now() < returnAt + 30_000 ? "available" : "locked" },
  }));
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures,
  } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    expect(result.current.trial.kind).toBe("active");
    // While this tab is in the background, an admin subscribes to Pro.
    // On return both reads refresh. The product policy cache still has 30s left.
    focusManager.setFocused(false);
    await advance(returnAt - daySeven);
    await act(async () => { focusManager.setFocused(true); });
    await advance(10);
    expect(result.current.trial.kind).toBe("none");
    expect(getWorkspaceFeatures).toHaveBeenCalled();
    await advance(60 * 60 * 1000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// The backoff recheck keeps going through failed attempts, not only through
// answers that are still active.
it("recovers the expiry summary after the server briefly fails the scheduled recheck", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(endsAt - 1_000);
  const attempts: number[] = [];
  const getWorkspaceSubscriptionSummary = async () => {
    const elapsed = Date.now() - endsAt;
    attempts.push(elapsed);
    // The browser is two minutes ahead. Both the first backoff request and
    // React Query's normal retry hit a temporary service error.
    if (elapsed >= 95_000 && elapsed < 110_000) throw new Error("HTTP 503");
    return elapsed < 120_000 ? active : expired;
  };
  setApiInstance({ getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => useWorkspaceTrial(ws, true), { wrapper });
  try {
    for (const ms of [1_000, 5, 69_995, 5, 30_000, 2_000, 60_000, 300_000, 3_600_000]) await advance(ms);
    expect(attempts.some((time) => time >= 95_000 && time < 110_000)).toBe(true);
    expect(result.current, JSON.stringify(attempts)).toMatchObject({ kind: "ended", confirmed: true });
  } finally {
    unmount();
    qc.clear();
  }
});

// A trial that converts at its end still gets the end's follow-up refetch: reads
// made just before the end may hold the trial's Business features.
it("refreshes Business-only feature access after a trial converts to Pro on another device", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(endsAt - 1_000);
  // The Pro summary is already committed. A previously cached Business
  // policy has half a second left when the local expiry timer fires.
  const featureReads: number[] = [];
  const getWorkspaceFeatures = async () => {
    featureReads.push(Date.now() - endsAt);
    return { features: { private_projects: Date.now() < endsAt + 500 ? "available" : "locked" } };
  };
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures,
  } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    for (const ms of [1_000, 5, 70_000, 3_600_000]) await advance(ms);
    expect(result.current.trial.kind).toBe("none");
    expect(result.current.access.state, JSON.stringify(featureReads)).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// A desktop window switches workspaces without remounting. Switching to
// another workspace's trial and back must not drop the refresh a mid-trial
// conversion scheduled for this one.
it("keeps a converted workspace's feature refresh across a workspace switch", async () => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  const other = "ws-trial-other";
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures: async () => ({
      features: { private_projects: Date.now() < t0 + 60_000 ? "available" : "locked" },
    }),
  } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  qc.setQueryData(workspaceSubscriptionKeys.summary(other), active);
  qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, rerender, unmount } = renderHook(
    ({ wsId }) => ({
      trial: useWorkspaceTrial(wsId, true),
      access: usePaidFeatureAccess(wsId, "private_projects", { hasExistingConfig: true }),
    }),
    { wrapper, initialProps: { wsId: ws } },
  );
  try {
    // This workspace's summary now reports Pro; the server still serves the
    // trial's Business policy for another minute.
    await act(async () => {
      await qc.invalidateQueries({ queryKey: workspaceSubscriptionKeys.all(ws) });
    });
    await advance(10);
    expect(result.current.trial.kind).toBe("none");

    rerender({ wsId: other });
    await advance(1_000);
    rerender({ wsId: ws });
    await advance(10_000);
    expect(result.current.access.state).toBe("available");

    await advance(80_000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// Subscribed on another device while this tab was in the background: coming
// back re-reads the plan (the summary is stale after 30 seconds), and the
// paid-feature read made alongside it is refreshed once the server's cached
// trial policy has expired.
it("converges an open tab to Pro's access after a refocus shortly after conversion", async () => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  const convertedAt = t0 + 30_000;
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => (Date.now() < convertedAt ? active : paid),
    getWorkspaceFeatures: async () => ({
      features: { private_projects: Date.now() < convertedAt + 60_000 ? "available" : "locked" },
    }),
  } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    focusManager.setFocused(false);
    await advance(60_000);
    await act(async () => { focusManager.setFocused(true); });
    await advance(10);
    expect(result.current.trial.kind).toBe("none");
    await advance(10 * 60 * 1000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// Opening the app just after the workspace converted: the summary already
// says Pro, but the first paid-feature read may come from the server's cached
// trial policy. The first read of the plan schedules one refresh too.
it("converges a freshly opened app to Pro's access", async () => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures: async () => ({
      features: { private_projects: Date.now() < t0 + 50_000 ? "available" : "locked" },
    }),
  } as unknown as ApiClient);
  const qc = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    await advance(10);
    expect(result.current.access.state).toBe("available");
    await advance(10 * 60 * 1000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// Coming back within the summary's stale time during a trial still re-reads
// the plan: it may have been bought on another device a moment ago.
it("re-reads the plan on refocus during a trial even while the summary is fresh", async () => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  const convertedAt = t0 + 15_000;
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => (Date.now() < convertedAt ? active : paid),
    getWorkspaceFeatures: async () => ({
      features: { private_projects: Date.now() < convertedAt + 60_000 ? "available" : "locked" },
    }),
  } as unknown as ApiClient);
  const qc = createQueryClient();
  qc.setQueryData(workspaceSubscriptionKeys.summary(ws), active);
  qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    focusManager.setFocused(false);
    await advance(20_000);
    await act(async () => { focusManager.setFocused(true); });
    await advance(10);
    expect(result.current.trial.kind).toBe("none");
    await advance(10 * 60 * 1000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// A paid-feature read that failed (first read, or the refresh after a plan
// change) is asked again with backoff once the service recovers.
it.each([
  ["the first read", 0, 10_000],
  ["the refresh after the plan change", 65_000, 80_000],
] as const)("recovers paid-feature access when %s fails", async (_name, failFrom, failUntil) => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures: async () => {
      const elapsed = Date.now() - t0;
      if (elapsed >= failFrom && elapsed < failUntil) throw new Error("HTTP 503");
      return { features: { private_projects: elapsed < 50_000 ? "available" : "locked" } };
    },
  } as unknown as ApiClient);
  const qc = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result, unmount } = renderHook(() => ({
    trial: useWorkspaceTrial(ws, true),
    access: usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
  }), { wrapper });
  try {
    // Step through ten minutes so renders between timers run, as in a page.
    for (let i = 0; i < 60; i++) await advance(10_000);
    expect(result.current.access.state).toBe("read_only");
  } finally {
    unmount();
    qc.clear();
  }
});

// A paid-feature read already in flight when the refresh runs (a project
// opened a moment before), which then fails: the refresh waits for it and
// retries with backoff instead of skipping it.
it.each([
  ["with an earlier answer cached", true],
  ["with nothing cached", false],
] as const)("retries a paid-feature read that was in flight at the refresh and then failed (%s)", async (_name, cached) => {
  vi.useFakeTimers();
  const t0 = endsAt - 7 * 24 * 60 * 60 * 1000;
  vi.setSystemTime(t0);
  setApiInstance({
    getWorkspaceSubscriptionSummary: async () => paid,
    getWorkspaceFeatures: async () => {
      const startedAt = Date.now() - t0;
      await new Promise((resolve) => setTimeout(resolve, 2_000));
      if (startedAt >= 69_000 && startedAt < 80_000) throw new Error("HTTP 503");
      return { features: { private_projects: startedAt < 50_000 ? "available" : "locked" } };
    },
  } as unknown as ApiClient);
  const qc = createQueryClient();
  if (cached) {
    qc.setQueryData(workspaceFeatureKeys.all(ws), { features: { private_projects: "available" } });
  }
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const shell = renderHook(() => useWorkspaceTrial(ws, true), { wrapper });
  let unmountPage: (() => void) | null = null;
  try {
    await advance(10);
    // The project opens at 69 s; its read is still in flight at the 70 s refresh.
    await advance(69_000);
    if (cached) {
      await act(async () => {
        await qc.invalidateQueries({ queryKey: workspaceFeatureKeys.all(ws) });
      });
    }
    const page = renderHook(
      () => usePaidFeatureAccess(ws, "private_projects", { hasExistingConfig: true }),
      { wrapper },
    );
    unmountPage = page.unmount;
    for (let i = 0; i < 60; i++) await advance(10_000);
    expect(page.result.current.state).toBe("read_only");
  } finally {
    unmountPage?.();
    shell.unmount();
    qc.clear();
  }
});

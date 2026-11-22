/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient, DashboardRequestParams } from "../api/client";
import type { WorkspaceFeatures } from "../types";
import {
  minHistoryLimit,
  resolveDashboardHistoryLimit,
  useDashboardHistoryLimit,
} from "./history-limit";
import { dashboardUsageByAgentOptions } from "./queries";

const features = (days: number | null): WorkspaceFeatures => ({
  features: {},
  limits: { dashboardHistoryDays: days },
});

/**
 * A server whose plan can change between reads: features and every dashboard
 * response report the same current limit, like one Multica instance would.
 */
function setup(initialLimit: number | null) {
  let limit = initialLimit;
  const api = {
    getWorkspaceFeatures: vi.fn(async () => features(limit)),
    getDashboardUsageByAgent: vi.fn(async (params: DashboardRequestParams) => {
      params.onHistoryLimit?.(limit);
      return [];
    }),
  };
  setApiInstance(api as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return {
    api,
    qc,
    wrapper,
    setPlanLimit: (next: number | null) => {
      limit = next;
    },
  };
}

/** The page's wiring: the dashboard query is made under the resolved limit. */
function useDashboard(wsId: string) {
  const history = useDashboardHistoryLimit(wsId);
  const query = useQuery(
    dashboardUsageByAgentOptions(wsId, 30, null, "UTC", history.ready ? history.maxDays : undefined),
  );
  return { history, query };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("resolveDashboardHistoryLimit", () => {
  it("lets a dashboard response newer than the features read tighten the limit", () => {
    expect(minHistoryLimit(null, 30)).toBe(30);
    expect(minHistoryLimit(90, null)).toBe(90);
    expect(minHistoryLimit(90, 30)).toBe(30);
    expect(resolveDashboardHistoryLimit(90, 100, undefined)).toBe(90);
    expect(resolveDashboardHistoryLimit(90, 100, { limitDays: 30, observedAt: 200 })).toBe(30);
    expect(resolveDashboardHistoryLimit(null, 100, { limitDays: 30, observedAt: 200 })).toBe(30);
    // An upgrade seen first in a response never widens before features agree.
    expect(resolveDashboardHistoryLimit(30, 100, { limitDays: null, observedAt: 200 })).toBe(30);
    // A features read newer than the observation decides alone.
    expect(resolveDashboardHistoryLimit(null, 300, { limitDays: 30, observedAt: 200 })).toBeNull();
  });
});

describe("useDashboardHistoryLimit", () => {
  it("does not query the dashboard until the limit is known", async () => {
    const { api, wrapper } = setup(30);
    let resolveFeatures: (value: WorkspaceFeatures) => void = () => {};
    api.getWorkspaceFeatures.mockImplementationOnce(
      () => new Promise<WorkspaceFeatures>((resolve) => (resolveFeatures = resolve)),
    );
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    expect(result.current.history.ready).toBe(false);
    expect(api.getDashboardUsageByAgent).not.toHaveBeenCalled();

    act(() => resolveFeatures(features(30)));
    await waitFor(() => expect(api.getDashboardUsageByAgent).toHaveBeenCalledTimes(1));
    expect(result.current.history).toEqual({ ready: true, maxDays: 30 });
  });

  it("still loads the dashboard when the features read fails", async () => {
    const { api, wrapper } = setup(null);
    api.getWorkspaceFeatures.mockRejectedValueOnce(new Error("offline"));
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(api.getDashboardUsageByAgent).toHaveBeenCalledTimes(1));
    expect(result.current.history).toEqual({ ready: true, maxDays: null });
  });

  it("notices a trial ending while the page stays open", async () => {
    const { api, qc, wrapper, setPlanLimit } = setup(null);
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
    expect(result.current.history.maxDays).toBeNull();
    const featureReads = api.getWorkspaceFeatures.mock.calls.length;

    // The trial ends; the features read is still cached. The next periodic
    // dashboard refetch is served under the new limit.
    setPlanLimit(30);
    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));

    await waitFor(() => expect(result.current.history.maxDays).toBe(30));
    await waitFor(() =>
      expect(api.getWorkspaceFeatures.mock.calls.length).toBeGreaterThan(featureReads),
    );
    // The query moved to the new limit's key and settled there.
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
    const lastCall = api.getDashboardUsageByAgent.mock.calls.length;
    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));
    expect(api.getDashboardUsageByAgent.mock.calls.length).toBe(lastCall + 1);
    expect(result.current.history.maxDays).toBe(30);
  });

  it("lifts the limit only once the features read confirms an upgrade", async () => {
    const { api, qc, wrapper, setPlanLimit } = setup(30);
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
    expect(result.current.history.maxDays).toBe(30);

    // An upgrade paid elsewhere. Hold the features refresh to see the
    // in-between state.
    setPlanLimit(null);
    let releaseFeatures: () => void = () => {};
    api.getWorkspaceFeatures.mockImplementationOnce(
      () =>
        new Promise<WorkspaceFeatures>((resolve) => {
          releaseFeatures = () => resolve(features(null));
        }),
    );
    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));
    expect(result.current.history.maxDays).toBe(30);

    act(() => releaseFeatures());
    await waitFor(() => expect(result.current.history.maxDays).toBeNull());
  });

  it("observe → enforce: a features read that turns the limit on re-keys the dashboard", async () => {
    const { api, qc, wrapper, setPlanLimit } = setup(null);
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
    const before = api.getDashboardUsageByAgent.mock.calls.length;

    setPlanLimit(90);
    await act(() => qc.invalidateQueries({ queryKey: ["workspace-features", "ws-1"] }));

    await waitFor(() => expect(result.current.history.maxDays).toBe(90));
    await waitFor(() => expect(api.getDashboardUsageByAgent.mock.calls.length).toBe(before + 1));
    // Not a placeholder from the unlimited key.
    expect(result.current.query.isPlaceholderData).toBe(false);
  });

  it("keeps the strictest limit seen until the features read confirms it", async () => {
    // Pro: 90 days. The plan drops to Free, but replicas disagree for a while:
    // a response under the new policy (30) arrives, then a late one served
    // under the old, unlimited policy.
    const { api, qc, wrapper } = setup(90);
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
    expect(result.current.history.maxDays).toBe(90);

    const servedQueue: (number | null)[] = [30, null];
    api.getDashboardUsageByAgent.mockImplementation(async (params: DashboardRequestParams) => {
      params.onHistoryLimit?.(servedQueue.length > 0 ? servedQueue.shift()! : 30);
      return [];
    });
    // Hold the features refresh so only dashboard responses speak.
    let releaseFeatures: () => void = () => {};
    api.getWorkspaceFeatures.mockImplementation(
      () =>
        new Promise<WorkspaceFeatures>((resolve) => {
          releaseFeatures = () => resolve(features(30));
        }),
    );

    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));
    await waitFor(() => expect(result.current.history.maxDays).toBe(30));
    // The late unlimited response, now under the 30-day key.
    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));
    expect(result.current.history.maxDays).toBe(30);

    act(() => releaseFeatures());
    await waitFor(() => expect(result.current.history.maxDays).toBe(30));
  });

  it("never caches rows cut to a stricter limit under the looser key", async () => {
    const { api, qc, wrapper, setPlanLimit } = setup(90);
    const { result } = renderHook(() => useDashboard("ws-1"), { wrapper });
    await waitFor(() => expect(result.current.query.isSuccess).toBe(true));

    // The 90-day query is refetched after the downgrade and served cut to 30.
    setPlanLimit(30);
    api.getDashboardUsageByAgent.mockImplementation(async (params: DashboardRequestParams) => {
      params.onHistoryLimit?.(30);
      return [{ sentinel: "cut-to-30" }] as never;
    });
    await act(() => qc.refetchQueries({ queryKey: ["dashboard", "ws-1"], type: "active" }));
    await waitFor(() => expect(result.current.history.maxDays).toBe(30));
    const ninetyKey = dashboardUsageByAgentOptions("ws-1", 30, null, "UTC", 90).queryKey;
    expect(qc.getQueryData(ninetyKey)).not.toEqual([{ sentinel: "cut-to-30" }]);

    // Upgrading back must fetch the 90-day key again, not reuse the cut rows.
    setPlanLimit(90);
    api.getDashboardUsageByAgent.mockImplementation(async (params: DashboardRequestParams) => {
      params.onHistoryLimit?.(90);
      return [{ sentinel: "full-90" }] as never;
    });
    await act(() => qc.invalidateQueries({ queryKey: ["workspace-features", "ws-1"] }));
    await waitFor(() => expect(result.current.history.maxDays).toBe(90));
    await waitFor(() => expect(result.current.query.data).toEqual([{ sentinel: "full-90" }]));
  });
});

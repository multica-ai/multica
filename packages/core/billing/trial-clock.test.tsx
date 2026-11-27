// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useWorkspaceTrial } from "./trial";
import { workspaceSubscriptionKeys } from "./workspace-subscription-queries";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// This clock runs ahead of Cloud's, so both refetches around the end still read
// the trial as active; the hook keeps asking until Cloud confirms.
it("confirms expiry in an open tab when the browser clock is two minutes ahead", async () => {
  vi.useFakeTimers();
  const endsAt = Date.parse("2030-01-15T00:00:00Z");
  vi.setSystemTime(endsAt - 1000);
  const trial = { plan: "business", startedAt: "2030-01-01T00:00:00Z", endsAt: new Date(endsAt).toISOString() };
  const active = { entitlement: { plan: "business", status: "trialing" }, trial: { ...trial, state: "active" } };
  const expired = { entitlement: { plan: "free", status: "inactive" }, trial: { ...trial, state: "expired" } };
  const clockOffset = 120_000;
  const getWorkspaceSubscriptionSummary = vi.fn(async () =>
    Date.now() - clockOffset < endsAt ? active : expired,
  );
  setApiInstance({ getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(workspaceSubscriptionKeys.summary("ws-clock"), active);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => useWorkspaceTrial("ws-clock", true), { wrapper });
  try {
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    await act(async () => { await vi.advanceTimersByTimeAsync(70_000); });
    expect(result.current).toMatchObject({ kind: "ended", confirmed: false });
    await act(async () => { await vi.advanceTimersByTimeAsync(60 * 60 * 1000); });
    expect(result.current).toMatchObject({ kind: "ended", confirmed: true });
  } finally {
    qc.clear();
  }
});

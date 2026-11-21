import { act, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { workspaceSubscriptionKeys, useTrialNoticeStore } from "@multica/core/billing";
import { renderWithI18n } from "../test/i18n";
import { WorkspaceTrialNotices } from "./trial-notices";

const h = vi.hoisted(() => ({ toast: vi.fn() }));
vi.mock("sonner", () => ({ toast: h.toast }));
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => true }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-member", name: "Test" }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: "member", isLoading: false }),
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

// The real trial hook and query client, with Cloud's summary answering late:
// the end on this clock is not a downgrade until Cloud says so.
it("does not tell a member the workspace is Free if an admin subscribed on another device", async () => {
  vi.useFakeTimers();
  const endsAt = Date.parse("2030-01-15T00:00:00Z");
  vi.setSystemTime(endsAt - 1000);
  useTrialNoticeStore.setState({ seen: [] });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const key = workspaceSubscriptionKeys.summary("ws-member");
  qc.setQueryData(key, {
    entitlement: { plan: "business", status: "trialing" },
    trial: { plan: "business", state: "active", startedAt: "2030-01-01T00:00:00Z", endsAt: new Date(endsAt).toISOString() },
  });
  const paid = { entitlement: { plan: "pro", status: "active" }, trial: null };
  const getWorkspaceSubscriptionSummary = vi.fn(() =>
    new Promise(resolve => setTimeout(() => resolve(paid), 100)),
  );
  setApiInstance({ getWorkspaceSubscriptionSummary } as unknown as ApiClient);
  renderWithI18n(<QueryClientProvider client={qc}><WorkspaceTrialNotices /></QueryClientProvider>);
  await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
  await act(async () => { await vi.advanceTimersByTimeAsync(200); });
  expect(qc.getQueryData(key)).toEqual(paid);
  expect(h.toast).not.toHaveBeenCalled();
});

import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";

interface AvailableActions {
  checkout: boolean;
  portal: boolean;
  purchaseSeats: boolean;
}

const mockPush = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockCreatePortal = vi.hoisted(() => vi.fn());
const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockSummaryQuery = vi.hoisted(() => vi.fn());
const featureState = vi.hoisted(() => ({ billingEnabled: true }));
const summaryState = vi.hoisted(() => ({
  value: null as null | { availableActions: AvailableActions },
  error: null as Error | null,
  pending: null as Promise<{ availableActions: AvailableActions } | null> | null,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/ws-test/settings",
  }),
}));

vi.mock("@multica/core/config", () => ({
  useFeatureEnabled: () => featureState.billingEnabled,
}));

vi.mock("../navigation/context", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("../platform", () => ({
  openExternal: mockOpenExternal,
}));

vi.mock("@multica/core/billing", () => ({
  workspaceSubscriptionSummaryOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "summary"],
    queryFn: mockSummaryQuery,
  }),
  useCreateWorkspaceSubscriptionPortal: () => ({
    mutateAsync: mockCreatePortal,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    error: mockToastError,
    dismiss: mockToastDismiss,
  },
}));

import { useIssueLimitUpgradePrompt } from "./use-issue-limit-upgrade-prompt";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals },
};

const actions = (
  overrides: Partial<AvailableActions> = {},
): AvailableActions => ({
  checkout: false,
  portal: false,
  purchaseSeats: false,
  ...overrides,
});

function renderPrompt(onNavigate = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: 1 } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nProvider>
  );
  const hook = renderHook(
    () => useIssueLimitUpgradePrompt({ onNavigate }),
    { wrapper },
  );
  return { ...hook, client, onNavigate };
}

function latestToastOptions() {
  return mockToastError.mock.calls.at(-1)?.[1];
}

describe("useIssueLimitUpgradePrompt", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    featureState.billingEnabled = true;
    summaryState.value = null;
    summaryState.error = null;
    summaryState.pending = null;
    mockSummaryQuery.mockImplementation(async () => {
      if (summaryState.pending) return summaryState.pending;
      if (summaryState.error) throw summaryState.error;
      return summaryState.value;
    });
    mockCreatePortal.mockResolvedValue({
      url: "https://billing.example/portal",
    });
  });

  it("shows a persistent recovery surface immediately while Cloud is pending", async () => {
    let resolveSummary!: (
      value: { availableActions: AvailableActions } | null,
    ) => void;
    summaryState.pending = new Promise((resolve) => {
      resolveSummary = resolve;
    });
    const { result } = renderPrompt();

    act(() => result.current());

    expect(mockToastError).toHaveBeenCalledWith(
      "This workspace has reached its issue limit",
      expect.objectContaining({
        description: "Checking the billing actions available for this workspace…",
        duration: Infinity,
        closeButton: true,
        onDismiss: expect.any(Function),
        onAutoClose: expect.any(Function),
      }),
    );

    await act(async () => {
      resolveSummary({ availableActions: actions({ checkout: true }) });
      await summaryState.pending;
    });
    await waitFor(() =>
      expect(latestToastOptions()?.action?.label).toBe("Upgrade to Pro"),
    );
  });

  it("does not restore the prompt after it is manually dismissed", async () => {
    let resolveSummary!: (
      value: { availableActions: AvailableActions } | null,
    ) => void;
    summaryState.pending = new Promise((resolve) => {
      resolveSummary = resolve;
    });
    const { result } = renderPrompt();

    act(() => result.current());
    act(() => latestToastOptions()?.onDismiss?.());

    await act(async () => {
      resolveSummary({ availableActions: actions({ checkout: true }) });
      await summaryState.pending;
      await Promise.resolve();
    });

    expect(mockToastError).toHaveBeenCalledTimes(1);
  });

  it("offers Upgrade to Pro only when Cloud authorizes checkout", async () => {
    summaryState.value = {
      availableActions: actions({ checkout: true }),
    };
    const { result, onNavigate } = renderPrompt();

    act(() => result.current());
    await waitFor(() =>
      expect(latestToastOptions()?.action?.label).toBe("Upgrade to Pro"),
    );

    latestToastOptions()?.action?.onClick();
    expect(mockToastDismiss).toHaveBeenCalledWith(
      "issue-limit-recovery:ws-test",
    );
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings?tab=billing");
  });

  it("opens Billing Portal for a past-due manager authorized for portal", async () => {
    summaryState.value = {
      availableActions: actions({ portal: true }),
    };
    const { result, onNavigate } = renderPrompt();

    act(() => result.current());
    await waitFor(() =>
      expect(latestToastOptions()?.action?.label).toBe("Open Billing Portal"),
    );

    await act(async () => {
      latestToastOptions()?.action?.onClick();
    });
    await waitFor(() => expect(mockCreatePortal).toHaveBeenCalledTimes(1));
    expect(mockCreatePortal.mock.calls[0]?.[0]).toMatch(
      /^issue-limit-portal-ws-test-/,
    );
    expect(mockToastDismiss).toHaveBeenCalledWith(
      "issue-limit-recovery:ws-test",
    );
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(mockOpenExternal).toHaveBeenCalledWith(
      "https://billing.example/portal",
      { webTarget: "same-tab" },
    );
  });

  it("asks for an administrator only when Cloud authorizes no management action", async () => {
    summaryState.value = { availableActions: actions() };
    const { result } = renderPrompt();

    act(() => result.current());
    await waitFor(() =>
      expect(latestToastOptions()?.description).toBe(
        "Ask a workspace owner or admin to upgrade to Pro.",
      ),
    );
    expect(latestToastOptions()).not.toHaveProperty("action");
  });

  it("keeps another Cloud-authorized management action reachable in Billing", async () => {
    summaryState.value = {
      availableActions: actions({ purchaseSeats: true }),
    };
    const { result } = renderPrompt();

    act(() => result.current());
    await waitFor(() =>
      expect(latestToastOptions()?.action?.label).toBe("View Billing"),
    );
  });

  it("uses one background attempt and keeps Billing as the recovery path", async () => {
    summaryState.error = new Error("cloud unavailable");
    const { result } = renderPrompt();

    act(() => result.current());
    expect(latestToastOptions()?.description).toBe(
      "Checking the billing actions available for this workspace…",
    );
    await waitFor(() =>
      expect(latestToastOptions()?.action?.label).toBe("View Billing"),
    );
    expect(mockSummaryQuery).toHaveBeenCalledTimes(1);
  });

  it("does not expose a dead Billing link when the Billing surface is disabled", () => {
    featureState.billingEnabled = false;
    const { result } = renderPrompt();

    act(() => result.current());

    expect(latestToastOptions()).toEqual(
      expect.objectContaining({
        description:
          "Delete an existing issue to free space, or contact your workspace administrator.",
        duration: Infinity,
        closeButton: true,
      }),
    );
    expect(latestToastOptions()).not.toHaveProperty("action");
    expect(mockSummaryQuery).not.toHaveBeenCalled();
  });
});

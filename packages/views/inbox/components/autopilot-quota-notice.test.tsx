import type { ReactElement } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import en from "../../locales/en/inbox.json";

const billingState = vi.hoisted(() => ({
  enabled: true,
  summary: null as null | {
    availableActions: Record<string, boolean>;
    entitlement: { plan: string };
    autopilotAddon: { purchasedRuns: number } | null;
  },
}));

vi.mock("@multica/core/config", () => ({
  useFeatureEnabled: () => billingState.enabled,
}));

vi.mock("@multica/core/billing", () => ({
  workspaceSubscriptionSummaryOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "summary"],
    queryFn: async () => billingState.summary,
  }),
}));

vi.mock("../../billing/autopilot-addon-dialog", () => ({
  AutopilotAddonDialog: ({ open }: { open: boolean }) =>
    open ? <div role="dialog" aria-label="Buy more automation runs" /> : null,
}));

import { AutopilotQuotaNotice } from "./autopilot-quota-notice";

function renderNotice(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

const allowed = (overrides: Record<string, boolean> = {}) => ({
  checkout: false,
  portal: false,
  purchaseSeats: false,
  changePlan: false,
  checkPlanChange: false,
  purchaseAutopilotRuns: false,
  ...overrides,
});

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, string>) => {
      const template = accessor(en);
      if (!params) return template;
      return template.replace(
        /\{\{(\w+)\}\}/g,
        (_, key: string) => params[key] ?? "",
      );
    },
  }),
}));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "system",
    actor_id: null,
    type: "autopilot_quota_exceeded",
    severity: "attention",
    issue_id: null,
    title: "Autopilot run limit reached",
    body: "Fallback body",
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-08-28T08:00:00Z",
    details: {
      limit: "100",
      reset_at: "2026-09-01T00:00:00Z",
    },
    ...overrides,
  };
}

describe("AutopilotQuotaNotice", () => {
  beforeEach(() => {
    billingState.enabled = true;
    billingState.summary = null;
  });

  it("shows localized rejection facts and opens the shared recovery surface", () => {
    billingState.enabled = false;
    const onOpenRecovery = vi.fn();

    renderNotice(
      <AutopilotQuotaNotice
        item={item()}
        onOpenRecovery={onOpenRecovery}
      />,
    );

    expect(screen.getByText(/limit of 100 autopilot runs/)).toBeInTheDocument();
    expect(screen.getByText(/upgrades or buys more runs/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View options" }));
    expect(onOpenRecovery).toHaveBeenCalledOnce();
  });

  it("offers an upgrade to a Free manager", async () => {
    billingState.summary = {
      availableActions: allowed({ checkout: true }),
      entitlement: { plan: "free" },
      autopilotAddon: null,
    };
    const onOpenRecovery = vi.fn();
    renderNotice(<AutopilotQuotaNotice item={item()} onOpenRecovery={onOpenRecovery} />);

    fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
    expect(onOpenRecovery).toHaveBeenCalledOnce();
  });

  it("opens the add-on purchase for a paid manager", async () => {
    billingState.summary = {
      availableActions: allowed({ portal: true, purchaseAutopilotRuns: true }),
      entitlement: { plan: "pro" },
      autopilotAddon: { purchasedRuns: 0 },
    };
    const onOpenRecovery = vi.fn();
    renderNotice(<AutopilotQuotaNotice item={item()} onOpenRecovery={onOpenRecovery} />);

    fireEvent.click(await screen.findByRole("button", { name: "Buy more runs" }));
    expect(screen.getByRole("dialog", { name: "Buy more automation runs" })).toBeInTheDocument();
    expect(onOpenRecovery).not.toHaveBeenCalled();
  });

  it.each([
    ["free", null, "Ask a workspace owner or admin to upgrade the plan."],
    ["pro", { purchasedRuns: 0 }, "Ask a workspace owner or admin to buy more runs or upgrade the plan."],
    ["business", { purchasedRuns: 0 }, "Ask a workspace owner or admin to buy more runs."],
  ])("tells a %s member who to ask, without a button", async (plan, autopilotAddon, text) => {
    billingState.summary = {
      availableActions: allowed(),
      entitlement: { plan },
      autopilotAddon,
    };
    renderNotice(<AutopilotQuotaNotice item={item()} onOpenRecovery={vi.fn()} />);

    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("keeps the first blocked autopilot out of the workspace-level body", () => {
    billingState.enabled = false;
    renderNotice(
      <AutopilotQuotaNotice
        item={item({
          details: {
            limit: "100",
            reset_at: "2026-09-01T00:00:00Z",
            autopilot_title: "Daily triage",
          },
        })}
        onOpenRecovery={vi.fn()}
      />,
    );

    expect(screen.queryByText(/Daily triage/)).not.toBeInTheDocument();
    expect(screen.getByText(/limit of 100 autopilot runs/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View options" })).toBeInTheDocument();
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import enCrm from "../../locales/en/crm.json";
import { CRMDashboardPage } from "./crm-dashboard-page";

const push = vi.fn();

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push }) }));
vi.mock("@multica/core/api", () => ({
  api: {
    listCRMAccounts: vi.fn(),
    listCRMEmailThreads: vi.fn(),
  },
}));

function renderDashboard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={{ en: { crm: enCrm } }}>
      <QueryClientProvider client={client}>
        <WorkspaceSlugProvider slug="acme">
          <CRMDashboardPage />
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const baseAccount = {
  workspace_id: "workspace-1",
  account_type: "prospect" as const,
  status: "active" as const,
  source: "manual" as const,
  rating: "hot" as const,
  priority: "high" as const,
  tags: [],
  contact_count: 1,
  next_follow_up_at: "2026-05-12T10:00:00Z",
  created_at: "2026-05-12T08:00:00Z",
  updated_at: "2026-05-12T09:00:00Z",
};

const account = (id: string, name: string) => ({ ...baseAccount, id, name });

describe("CRMDashboardPage", () => {
  beforeEach(() => {
    push.mockReset();
    vi.mocked(api.listCRMAccounts).mockImplementation(async (params) => {
      if (params?.follow_up_bucket === "overdue") return { accounts: [account("follow-account", "Follow Customer")], total: 1 };
      if (params?.rating === "hot") return { accounts: [account("hot-account", "Hot Customer")], total: 1 };
      return { accounts: [account("recent-account", "Recent Customer")], total: 1 };
    });
    vi.mocked(api.listCRMEmailThreads).mockResolvedValue({
      threads: [{ id: "thread-1", workspace_id: "workspace-1", subject: "RFQ", direction: "inbound", status: "open", message_count: 1, created_at: "2026-05-12T07:00:00Z", updated_at: "2026-05-12T08:00:00Z", last_message_at: "2026-05-12T08:00:00Z" }],
      total: 1,
    });
  });

  it("renders CRM dashboard action sections", async () => {
    renderDashboard();

    expect(await screen.findByRole("heading", { name: "CRM Dashboard" })).toBeInTheDocument();
    expect(screen.getByText("Needs follow-up")).toBeInTheDocument();
    expect(screen.getAllByText("Hot customers").length).toBeGreaterThan(0);
    expect(screen.getByText("Recent customers")).toBeInTheDocument();
    expect(screen.getByText("Recent emails")).toBeInTheDocument();
    expect(await screen.findByText("Follow Customer")).toBeInTheDocument();
    expect(await screen.findByText("Hot Customer")).toBeInTheDocument();
    expect(await screen.findByText("Recent Customer")).toBeInTheDocument();
    expect(screen.getByText("RFQ")).toBeInTheDocument();
  });

  it("navigates to customer and email work areas", async () => {
    renderDashboard();

    await screen.findByText("Follow Customer");
    const followButton = screen.getByText("Follow Customer").closest("button");
    expect(followButton).not.toBeNull();
    await userEvent.click(followButton!);
    expect(push).toHaveBeenCalledWith("/acme/crm/customers/follow-account");

    await userEvent.click(screen.getByRole("button", { name: "Emails" }));
    expect(push).toHaveBeenCalledWith("/acme/crm/emails");
  });
});

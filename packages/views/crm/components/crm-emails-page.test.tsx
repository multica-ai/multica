import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { beforeEach, describe, expect, it, vi } from "vitest";
import enCrm from "../../locales/en/crm.json";
import { CRMEmailsPage } from "./crm-emails-page";

const mockApi = vi.hoisted(() => ({
  listCRMEmailThreads: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderEmailsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={{ en: { crm: enCrm } }}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="acme">
          <CRMEmailsPage />
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockApi.listCRMEmailThreads.mockResolvedValue({
    threads: [
      {
        id: "thread-1",
        workspace_id: "ws-1",
        account_id: "account-1",
        contact_id: null,
        subject: "New quotation request",
        external_thread_id: null,
        mailbox: "sales@example.com",
        direction: "inbound",
        status: "open",
        last_message_at: "2026-05-12T10:00:00Z",
        message_count: 2,
        created_at: "2026-05-12T10:00:00Z",
        updated_at: "2026-05-12T10:00:00Z",
      },
    ],
    total: 1,
  });
});

describe("CRMEmailsPage", () => {
  it("renders customer email threads from the CRM email API", async () => {
    renderEmailsPage();

    expect(await screen.findByText("New quotation request")).toBeInTheDocument();
    expect(screen.getByText(/sales@example.com/)).toBeInTheDocument();
    expect(screen.getByText(/inbound/)).toBeInTheDocument();
    expect(screen.getByText(/2 messages/)).toBeInTheDocument();
    expect(mockApi.listCRMEmailThreads).toHaveBeenCalledWith(undefined);
  });
});

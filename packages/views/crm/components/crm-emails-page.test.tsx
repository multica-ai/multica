import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider } from "../../navigation";
import { beforeEach, describe, expect, it, vi } from "vitest";
import enCrm from "../../locales/en/crm.json";
import { CRMEmailsPage } from "./crm-emails-page";

const mockApi = vi.hoisted(() => ({
  listCRMEmailThreads: vi.fn(),
  listCRMAccounts: vi.fn(),
  listCRMContacts: vi.fn(),
  listCRMEmailMessages: vi.fn(),
  updateCRMEmailThreadAssociation: vi.fn(),
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
          <NavigationProvider value={{ push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/crm/emails", searchParams: new URLSearchParams() }}>
            <CRMEmailsPage />
          </NavigationProvider>
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
        contact_id: "contact-1",
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
  mockApi.listCRMAccounts.mockResolvedValue({
    accounts: [{ id: "account-1", workspace_id: "ws-1", name: "Acme Buyer", account_type: "customer", status: "active", rating: "hot", priority: "high", tags: [], contact_count: 1, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }],
    total: 1,
  });
  mockApi.listCRMContacts.mockResolvedValue({
    contacts: [{ id: "contact-1", workspace_id: "ws-1", account_id: "account-1", name: "Alice", is_primary: true, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }],
    total: 1,
  });
  mockApi.listCRMEmailMessages.mockResolvedValue({
    messages: [{ id: "message-1", workspace_id: "ws-1", thread_id: "thread-1", account_id: "account-1", contact_id: "contact-1", from_email: "buyer@example.com", from_name: "Alice", to_emails: ["sales@example.com"], cc_emails: [], bcc_emails: [], subject: "New quotation request", sent_at: "2026-05-12T10:00:00Z", received_at: null, body_text: "Please quote 500 units.", body_html: null, snippet: "Please quote", direction: "inbound", created_at: "2026-05-12T10:00:00Z", updated_at: "2026-05-12T10:00:00Z" }],
    total: 1,
  });
  mockApi.updateCRMEmailThreadAssociation.mockResolvedValue({ id: "thread-1", workspace_id: "ws-1", account_id: "account-1", contact_id: "contact-1", subject: "New quotation request", mailbox: "sales@example.com", direction: "inbound", status: "open", last_message_at: "2026-05-12T10:00:00Z", message_count: 2, created_at: "2026-05-12T10:00:00Z", updated_at: "2026-05-12T10:00:00Z" });
});

describe("CRMEmailsPage", () => {
  it("renders customer email threads from the CRM email API", async () => {
    renderEmailsPage();

    expect((await screen.findAllByText("New quotation request")).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/sales@example.com/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/inbound/).length).toBeGreaterThan(0);
    expect(screen.getByText(/2 messages/)).toBeInTheDocument();
    expect(mockApi.listCRMEmailThreads).toHaveBeenCalledWith(undefined);
    expect(await screen.findByText("Please quote 500 units.")).toBeInTheDocument();
    expect(screen.getByText("Acme Buyer · Alice")).toBeInTheDocument();
  });

  it("updates the selected email thread customer association", async () => {
    renderEmailsPage();

    await screen.findAllByText("New quotation request");
    await userEvent.click(screen.getByRole("button", { name: "Save link" }));

    expect(mockApi.updateCRMEmailThreadAssociation).toHaveBeenCalledWith("thread-1", {
      account_id: "account-1",
      contact_id: "contact-1",
    });
  });
});

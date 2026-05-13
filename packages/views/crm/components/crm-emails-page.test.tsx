import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
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
  createCRMContact: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const navigation = { push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/crm/emails", searchParams: new URLSearchParams() };

function renderEmailsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={{ en: { crm: enCrm } }}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="acme">
          <NavigationProvider value={navigation}>
            <CRMEmailsPage />
          </NavigationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const linkedThread = {
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
};

const unlinkedThread = {
  ...linkedThread,
  id: "thread-2",
  account_id: null,
  contact_id: null,
  subject: "Unlinked buyer email",
  message_count: 1,
};

const sentThread = {
  ...linkedThread,
  id: "thread-3",
  subject: "Sent quotation",
  direction: "outbound",
};

const account = {
  id: "account-1",
  workspace_id: "ws-1",
  name: "Acme Buyer",
  account_type: "customer",
  status: "active",
  rating: "hot",
  priority: "high",
  country_name: "United States",
  website: "https://acme.example",
  tags: [],
  contact_count: 1,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const contact = {
  id: "contact-1",
  workspace_id: "ws-1",
  account_id: "account-1",
  name: "Alice",
  email: "buyer@example.com",
  is_primary: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  navigation.push.mockClear();
  mockApi.listCRMEmailThreads.mockResolvedValue({ threads: [linkedThread], total: 1 });
  mockApi.listCRMAccounts.mockResolvedValue({ accounts: [account], total: 1 });
  mockApi.listCRMContacts.mockResolvedValue({ contacts: [contact], total: 1 });
  mockApi.listCRMEmailMessages.mockResolvedValue({
    messages: [{ id: "message-1", workspace_id: "ws-1", thread_id: "thread-1", account_id: "account-1", contact_id: "contact-1", from_email: "buyer@example.com", from_name: "Alice", to_emails: ["sales@example.com"], cc_emails: [], bcc_emails: [], subject: "New quotation request", sent_at: "2026-05-12T10:00:00Z", received_at: null, body_text: "Please quote 500 units.", body_html: null, snippet: "Please quote", direction: "inbound", created_at: "2026-05-12T10:00:00Z", updated_at: "2026-05-12T10:00:00Z" }],
    total: 1,
  });
  mockApi.updateCRMEmailThreadAssociation.mockResolvedValue(linkedThread);
  mockApi.createCRMContact.mockResolvedValue({ ...contact, id: "contact-2", name: "Bob", email: "bob@example.com" });
});

describe("CRMEmailsPage", () => {
  it("renders a CRM-style email workspace with folders, detail, context, and message body", async () => {
    renderEmailsPage();

    expect(await screen.findByText("Email workspace")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Email folders" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Inbox/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Mailbox settings/ })).toBeInTheDocument();
    expect((await screen.findAllByText("New quotation request")).length).toBeGreaterThan(0);
    expect(screen.getByText("CRM context")).toBeInTheDocument();
    expect(screen.getByText("Related project")).toBeInTheDocument();
    expect(screen.getByText("Related issue")).toBeInTheDocument();
    expect(screen.getAllByText(/sales@example.com/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/inbound/).length).toBeGreaterThan(0);
    expect(screen.getByText(/2 messages/)).toBeInTheDocument();
    expect(mockApi.listCRMEmailThreads).toHaveBeenCalledWith(undefined);
    expect(screen.getByRole("button", { name: /Mark read/ })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Archive/ }).length).toBeGreaterThan(0);
    expect(screen.getAllByRole("button", { name: /Star/ }).length).toBeGreaterThan(0);
    expect(await screen.findByText("Please quote 500 units.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Linked customer\s*Acme Buyer/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Linked contact\s*Alice/ })).toBeInTheDocument();
  });

  it("filters folders and opens the IMAP mailbox binding dialog", async () => {
    mockApi.listCRMEmailThreads.mockResolvedValue({ threads: [linkedThread, sentThread, unlinkedThread], total: 3 });
    renderEmailsPage();

    expect((await screen.findAllByText("New quotation request")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Sent quotation")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Sent/ }));
    expect((await screen.findAllByText("Sent quotation")).length).toBeGreaterThan(0);
    expect(screen.queryByText("New quotation request")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Mailbox settings/ }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("Bind an IMAP mailbox for CRM email import. Automatic sync stays off until the connection and security model are confirmed.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("IMAP host")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Secret reference")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Test connection" })).toBeInTheDocument();
  });

  it("opens CRM detail dialogs from linked customer and contact names", async () => {
    renderEmailsPage();

    await screen.findByText("Please quote 500 units.");
    await userEvent.click(screen.getByRole("button", { name: /Linked customer\s*Acme Buyer/ }));
    const customerDialog = screen.getByRole("dialog");
    expect(within(customerDialog).getByText("Customer details from CRM.")).toBeInTheDocument();
    expect(within(customerDialog).getByText("United States")).toBeInTheDocument();
    expect(within(customerDialog).getByText("Hot")).toBeInTheDocument();

    await userEvent.click(within(customerDialog).getByRole("button", { name: "Cancel" }));
    await userEvent.click(screen.getByRole("button", { name: /Linked contact\s*Alice/ }));
    const contactDialog = screen.getByRole("dialog");
    expect(within(contactDialog).getByText("Contact details from CRM.")).toBeInTheDocument();
    expect(within(contactDialog).getByText("buyer@example.com")).toBeInTheDocument();
  });

  it("links an unassociated email thread and creates a contact from the sender", async () => {
    mockApi.listCRMEmailThreads.mockResolvedValue({ threads: [unlinkedThread], total: 1 });
    mockApi.listCRMContacts.mockResolvedValue({ contacts: [], total: 0 });
    mockApi.listCRMEmailMessages.mockResolvedValue({
      messages: [{ id: "message-2", workspace_id: "ws-1", thread_id: "thread-2", account_id: null, contact_id: null, from_email: "bob@example.com", from_name: "Bob", to_emails: ["sales@example.com"], cc_emails: [], bcc_emails: [], subject: "Unlinked buyer email", sent_at: "2026-05-12T10:00:00Z", received_at: null, body_text: "Can you send the catalog?", body_html: null, snippet: "catalog", direction: "inbound", created_at: "2026-05-12T10:00:00Z", updated_at: "2026-05-12T10:00:00Z" }],
      total: 1,
    });
    mockApi.updateCRMEmailThreadAssociation.mockResolvedValue({ ...unlinkedThread, account_id: "account-1", contact_id: "contact-2" });

    renderEmailsPage();

    await screen.findByText("Can you send the catalog?");
    await userEvent.click(screen.getByRole("button", { name: "Link customer" }));
    const dialog = screen.getByRole("dialog");
    await userEvent.selectOptions(within(dialog).getByLabelText("Linked customer"), "account-1");

    expect(within(dialog).getByLabelText("Name")).toHaveValue("Bob");
    expect(within(dialog).getByLabelText("Email")).toHaveValue("bob@example.com");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save link" }));

    expect(mockApi.createCRMContact).toHaveBeenCalledWith("account-1", expect.objectContaining({
      account_id: "account-1",
      name: "Bob",
      email: "bob@example.com",
      is_primary: false,
    }));
    expect(mockApi.updateCRMEmailThreadAssociation).toHaveBeenCalledWith("thread-2", {
      account_id: "account-1",
      contact_id: "contact-2",
    });
  });
});

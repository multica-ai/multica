import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { NavigationProvider } from "../../navigation";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { describe, expect, it, vi, beforeEach } from "vitest";
import enCrm from "../../locales/en/crm.json";
import { CRMAccountDetailPage } from "./crm-account-detail-page";

const mockAccount = {
  id: "account-1",
  workspace_id: "ws-1",
  name: "Olrid Customer",
  normalized_name: "olrid customer",
  account_code: null,
  account_type: "customer",
  website: "https://example.com",
  country: "US",
  country_code: "US",
  country_name: "United States",
  region: "California",
  city: "San Francisco",
  industry: "Import",
  sub_industry: null,
  status: "active",
  owner_id: null,
  owner_member_id: null,
  source: "manual",
  rating: "warm",
  priority: "medium",
  annual_revenue: null,
  employee_count: null,
  tags: [],
  notes: "Important customer",
  last_contacted_at: null,
  next_follow_up_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  contact_count: 0,
} as const;

const projects = [
  {
    id: "project-1",
    workspace_id: "ws-1",
    title: "Linked project",
    description: null,
    icon: null,
    status: "planned",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 2,
    done_count: 0,
    resource_count: 1,
    resources: [{ id: "resource-1", project_id: "project-1", workspace_id: "ws-1", resource_type: "crm_account", resource_ref: { account_id: "account-1" }, label: "Olrid Customer", position: 0, created_at: "2026-01-01T00:00:00Z", created_by: null }],
  },
  {
    id: "project-2",
    workspace_id: "ws-1",
    title: "Candidate project",
    description: null,
    icon: null,
    status: "planned",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 1,
    done_count: 0,
    resource_count: 0,
    resources: [],
  },
] as const;

const mockModalOpen = vi.hoisted(() => vi.fn());
const mockIssueDraftStore = vi.hoisted(() => ({
  clearDraft: vi.fn(),
  setDraft: vi.fn(),
}));

const mockApi = vi.hoisted(() => ({
  getCRMAccount: vi.fn(),
  listCRMContacts: vi.fn(),
  listCRMCommunicationNotes: vi.fn(),
  listCRMEmailThreads: vi.fn(),
  listProjects: vi.fn(),
  listIssues: vi.fn(),
  linkCRMAccountProject: vi.fn(),
  createProjectResource: vi.fn(),
  deleteProjectResource: vi.fn(),
  createProject: vi.fn(),
  createCRMFollowUpIssue: vi.fn(),
  updateCRMAccount: vi.fn(),
  deleteCRMAccount: vi.fn(),
  createCRMContact: vi.fn(),
  updateCRMContact: vi.fn(),
  deleteCRMContact: vi.fn(),
  createCRMCommunicationNote: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: (selector: (state: { open: typeof mockModalOpen }) => unknown) => selector({ open: mockModalOpen }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: (selector: (state: typeof mockIssueDraftStore) => unknown) => selector(mockIssueDraftStore),
}));

const projectDraftStore = vi.hoisted(() => ({
  draft: {
    title: "",
    description: "",
    status: "planned",
    priority: "medium",
    leadType: undefined,
    leadId: undefined,
    icon: undefined,
  },
  setDraft: vi.fn((patch) => {
    projectDraftStore.draft = { ...projectDraftStore.draft, ...patch };
  }),
  clearDraft: vi.fn(() => {
    projectDraftStore.draft = {
      title: "",
      description: "",
      status: "planned",
      priority: "medium",
      leadType: undefined,
      leadId: undefined,
      icon: undefined,
    };
  }),
}));

vi.mock("@multica/core/projects", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/projects")>("@multica/core/projects");
  return {
    ...actual,
    useProjectDraftStore: (selector: (state: typeof projectDraftStore) => unknown) => selector(projectDraftStore),
  };
});

vi.mock("../../modals/create-project", () => ({
  CreateProjectModal: ({ initialResources }: { initialResources?: unknown[] }) => (
    <div role="dialog" aria-label="Create project modal">
      <input aria-label="Project title" defaultValue={projectDraftStore.draft.title} />
      <div data-testid="initial-resources">{JSON.stringify(initialResources ?? [])}</div>
    </div>
  ),
}));

function renderDetail() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const navigation = { push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/crm/customers/account-1", searchParams: new URLSearchParams() };

  render(
    <I18nProvider locale="en" resources={{ en: { crm: enCrm } }}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="acme">
          <NavigationProvider value={navigation}>
            <CRMAccountDetailPage accountId="account-1" />
          </NavigationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );

  return { navigation };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockApi.getCRMAccount.mockResolvedValue(mockAccount);
  mockApi.listCRMContacts.mockResolvedValue({ contacts: [], total: 0 });
  mockApi.listCRMCommunicationNotes.mockResolvedValue({ notes: [], total: 0 });
  mockApi.listCRMEmailThreads.mockResolvedValue({ threads: [], total: 0 });
  mockApi.listProjects.mockResolvedValue({ projects: [...projects], total: projects.length });
  mockApi.listIssues.mockResolvedValue({ issues: [{ id: "issue-1", identifier: "ISS-1", title: "Call customer", project_id: "project-1" }], total: 1 });
  mockApi.linkCRMAccountProject.mockResolvedValue({ resources: [], total: 0, skipped_project_ids: [] });
  mockApi.createProjectResource.mockResolvedValue({ id: "resource-2" });
  mockApi.deleteProjectResource.mockResolvedValue(undefined);
  mockApi.createProject.mockResolvedValue({ ...projects[1], id: "project-3", title: "CRM:Olrid Customer" });
  mockApi.createCRMFollowUpIssue.mockResolvedValue({ issue: { id: "issue-2" } });
  mockIssueDraftStore.clearDraft.mockClear();
  mockIssueDraftStore.setDraft.mockClear();
  mockModalOpen.mockClear();
});

describe("CRMAccountDetailPage", () => {
  it("renders a breadcrumb back to the customer list instead of a standalone back button", async () => {
    const { navigation } = renderDetail();

    await screen.findByRole("heading", { name: "Olrid Customer" });
    const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(breadcrumb).toHaveTextContent("Customers");
    expect(breadcrumb).toHaveTextContent("Olrid Customer");

    await userEvent.click(screen.getByRole("button", { name: /Customers/i }));

    expect(navigation.push).toHaveBeenCalledWith("/acme/crm/customers");
  });

  it("preselects stored country, region, and city values when editing a customer", async () => {
    renderDetail();

    await screen.findByRole("heading", { name: "Olrid Customer" });
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));

    expect(await screen.findByRole("combobox", { name: "Country" })).toHaveValue("US");
    expect(await screen.findByRole("combobox", { name: "Region" })).toHaveValue("CA");
    const citySelect = await screen.findByRole("combobox", { name: "City" });
    expect(citySelect).toHaveDisplayValue("San Francisco");
  });


  it("cascades country, region, and city selectors when editing a customer", async () => {
    renderDetail();

    await screen.findByRole("heading", { name: "Olrid Customer" });
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));

    await userEvent.selectOptions(await screen.findByRole("combobox", { name: "Country" }), "CN");
    expect(screen.getByRole("combobox", { name: "Region" })).toHaveValue("");
    expect(screen.getByRole("combobox", { name: "City" })).toHaveValue("");

    expect(await screen.findByRole("option", { name: "Guangdong" })).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Region" }), "GD");

    expect(await screen.findByRole("option", { name: "Shenzhen" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "City" })).toBeEnabled();
  });

  it("persists one-click project selection toggles immediately", async () => {
    renderDetail();

    await userEvent.click(await screen.findByRole("tab", { name: "Projects" }));
    const linkedProject = screen.getByRole("checkbox", { name: /Linked project/i });
    const candidateProject = screen.getByRole("checkbox", { name: /Candidate project/i });
    const linkedProjectRow = linkedProject.closest("button");
    const candidateProjectRow = candidateProject.closest("button");

    expect(linkedProject).toBeChecked();
    expect(candidateProject).not.toBeChecked();

    expect(candidateProjectRow).not.toBeNull();
    await userEvent.click(candidateProjectRow!);
    expect(candidateProject).toBeChecked();
    expect(mockApi.createProjectResource).toHaveBeenCalledWith("project-2", expect.objectContaining({
      resource_type: "crm_account",
      resource_ref: { account_id: "account-1", name: "Olrid Customer" },
      label: "Olrid Customer",
    }));

    expect(linkedProjectRow).not.toBeNull();
    await userEvent.click(linkedProjectRow!);
    expect(linkedProject).not.toBeChecked();
    expect(mockApi.deleteProjectResource).toHaveBeenCalledWith("project-1", "resource-1");
    expect(mockApi.linkCRMAccountProject).not.toHaveBeenCalled();
  });

  it("shows existing issues for the selected linked project and opens the system issue dialog for new follow-ups", async () => {
    renderDetail();

    await userEvent.click(await screen.findByRole("tab", { name: "Projects" }));
    await userEvent.selectOptions(screen.getByRole("combobox", { name: /Select project/i }), "project-1");

    expect(await screen.findByText("ISS-1 · Call customer")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("Follow up: Olrid Customer")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Create follow-up issue" }));

    expect(mockApi.listIssues).toHaveBeenCalledWith({ project_id: "project-1", open_only: true, limit: 50 });
    expect(mockApi.createCRMFollowUpIssue).not.toHaveBeenCalled();
    expect(mockIssueDraftStore.clearDraft).toHaveBeenCalled();
    expect(mockIssueDraftStore.setDraft).toHaveBeenCalledWith(expect.objectContaining({
      title: "Follow up: Olrid Customer",
      priority: "medium",
    }));
    expect(mockModalOpen).toHaveBeenCalledWith("create-issue", { project_id: "project-1" });
  });

  it("opens the project creation modal with a unique default CRM title and account resource", async () => {
    renderDetail();

    await userEvent.click(await screen.findByRole("tab", { name: "Projects" }));
    await userEvent.click(screen.getByRole("button", { name: /Create linked project/i }));

    expect(projectDraftStore.clearDraft).toHaveBeenCalled();
    expect(projectDraftStore.setDraft).toHaveBeenCalledWith(expect.objectContaining({
      title: "CRM:Olrid Customer",
      description: "Important customer",
      status: "planned",
      priority: "medium",
    }));
    expect(screen.getByRole("dialog", { name: "Create project modal" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Project title" })).toHaveValue("CRM:Olrid Customer");
    expect(screen.getByTestId("initial-resources")).toHaveTextContent("account-1");
    expect(mockApi.createProject).not.toHaveBeenCalled();
  });
});

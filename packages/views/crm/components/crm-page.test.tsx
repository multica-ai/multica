import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { describe, expect, it, vi } from "vitest";
import enCrm from "../../locales/en/crm.json";
import { CRMPage } from "./crm-page";

vi.mock("@multica/core/crm", () => ({
  crmAccountQueryOptions: () => ({
    queryKey: ["crm", "accounts", "test"],
    queryFn: async () => ({ accounts: [], total: 0 }),
  }),
  useCreateCRMAccountMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCreateCRMContactMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCreateCRMCommunicationNoteMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useLinkCRMAccountProjectsMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCreateCRMFollowUpIssueMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCreateCRMLinkedProjectMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/projects", () => ({
  projectQueryOptions: () => ({
    queryKey: ["projects", "test"],
    queryFn: async () => ({ projects: [], total: 0 }),
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderCRMPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={{ en: { crm: enCrm } }}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="acme">
          <CRMPage />
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("CRMPage", () => {
  it("renders translated customer text instead of blank chrome", async () => {
    renderCRMPage();

    expect(await screen.findByRole("heading", { name: "Customers" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add customer/i })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Search customers")).toBeInTheDocument();
    expect(await screen.findByText("No customers yet.")).toBeInTheDocument();
  });
});

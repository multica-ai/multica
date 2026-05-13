import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider } from "../../navigation";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import enCrm from "../../locales/en/crm.json";
import zhCrm from "../../locales/zh-Hans/crm.json";
import { CRMPage } from "./crm-page";

const mockAccounts = [
  {
    id: "account-1",
    workspace_id: "ws-1",
    name: "High Frequency",
    normalized_name: "high frequency",
    account_code: null,
    account_type: "customer",
    website: null,
    country: "CN",
    country_code: "CN",
    country_name: "China",
    region: null,
    city: null,
    industry: "Consumer Goods",
    sub_industry: "Home & Garden",
    status: "active",
    owner_id: null,
    owner_member_id: null,
    source: "manual",
    rating: "warm",
    priority: "medium",
    annual_revenue: null,
    employee_count: null,
    tags: ["VIP", "Distributor"],
    notes: null,
    last_contacted_at: null,
    next_follow_up_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    contact_count: 0,
  },
  {
    id: "account-2",
    workspace_id: "ws-1",
    name: "Another Customer",
    normalized_name: "another customer",
    account_code: null,
    account_type: "prospect",
    website: null,
    country: "US",
    country_code: "US",
    country_name: "United States",
    region: null,
    city: null,
    industry: "Retail",
    sub_industry: "E-commerce",
    status: "active",
    owner_id: null,
    owner_member_id: null,
    source: "manual",
    rating: "unknown",
    priority: "medium",
    annual_revenue: null,
    employee_count: null,
    tags: ["VIP"],
    notes: null,
    last_contacted_at: null,
    next_follow_up_at: null,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    contact_count: 0,
  },
] as const;

const mockApi = vi.hoisted(() => ({
  listCRMAccounts: vi.fn(),
  createCRMAccount: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("../geo", async () => {
  const actual = await vi.importActual<typeof import("../geo")>("../geo");
  return {
    ...actual,
    COUNTRY_OPTIONS: [
      { code: "US", name: { en: "United States", zh: "美国" }, regions: [] },
      { code: "CN", name: { en: "China", zh: "中国" }, regions: [] },
    ],
    loadRegionOptions: vi.fn(async (countryCode: string) => countryCode === "US" ? [
      { code: "CA", name: { en: "California", zh: "California" }, cities: [] },
      { code: "NY", name: { en: "New York", zh: "New York" }, cities: [] },
    ] : []),
    loadCityOptions: vi.fn(async (countryCode: string, regionCode: string) => countryCode === "US" && regionCode === "CA" ? [
      { code: "Acalanes Ridge", name: { en: "Acalanes Ridge", zh: "Acalanes Ridge" } },
      { code: "Adelanto", name: { en: "Adelanto", zh: "Adelanto" } },
    ] : []),
  };
});

function renderCRMPage(locale: "en" | "zh-Hans" = "en") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const navigation = { push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/crm/customers", searchParams: new URLSearchParams() };
  return render(
    <I18nProvider locale={locale} resources={{ en: { crm: enCrm }, "zh-Hans": { crm: zhCrm } }}>
      <QueryClientProvider client={queryClient}>
        <WorkspaceSlugProvider slug="acme">
          <NavigationProvider value={navigation}>
            <CRMPage />
          </NavigationProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.setSystemTime(new Date(2026, 4, 12, 10, 47, 0));
  vi.clearAllMocks();
  mockApi.listCRMAccounts.mockResolvedValue({ accounts: [...mockAccounts], total: mockAccounts.length });
  mockApi.createCRMAccount.mockResolvedValue({ ...mockAccounts[0], id: "account-3", name: "Created Customer" });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("CRMPage", () => {
  it("renders translated customer text instead of blank chrome", async () => {
    renderCRMPage();

    expect(await screen.findByRole("heading", { name: "Customers" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add customer/i })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Search customers")).toBeInTheDocument();
    expect(await screen.findByText("High Frequency")).toBeInTheDocument();
  });

  it("cascades country, region, and city selectors in the add customer dialog", async () => {
    renderCRMPage();

    await userEvent.click(await screen.findByRole("button", { name: /Add customer/i }));
    await userEvent.selectOptions(screen.getByLabelText("Country"), "US");

    expect(await screen.findByRole("option", { name: "California" })).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText("Region"), "CA");

    expect(await screen.findByRole("option", { name: "Acalanes Ridge" })).toBeInTheDocument();
    expect(screen.getByLabelText("City")).toBeEnabled();
  });

  it("uses linked industry and sub-industry dropdowns, tag frequency suggestions, and current datetime defaults", async () => {
    renderCRMPage();

    await userEvent.click(await screen.findByRole("button", { name: /Add customer/i }));

    expect(screen.getByLabelText("Next follow-up")).toHaveValue("2026-05-12T10:47");
    expect(screen.getByRole("button", { name: "VIP" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Distributor" })).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText("Industry"), "Consumer Goods");
    expect(await screen.findByRole("option", { name: "Home & Garden" })).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText("Sub-industry"), "Home & Garden");

    await userEvent.click(screen.getByRole("button", { name: "VIP" }));
    expect(screen.getByLabelText("Tags")).toHaveValue("VIP");
  });

  it("localizes sub-industry options with the system language", async () => {
    renderCRMPage("zh-Hans");

    await userEvent.click(await screen.findByRole("button", { name: /添加客户/i }));
    await userEvent.selectOptions(screen.getByLabelText("行业"), "Consumer Goods");

    expect(await screen.findByRole("option", { name: "家居园艺" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Home & Garden" })).not.toBeInTheDocument();
  });
});

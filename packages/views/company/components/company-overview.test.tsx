import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { CompanyOverview } from "./company-overview";
import { readCompanySelection } from "../lib/company-selection";

type QueryResult = { data?: unknown };

const mocks = vi.hoisted(() => ({
  queries: {} as Record<string, QueryResult>,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => ({
    isPending: false,
    ...mocks.queries[(options.queryKey ?? []).join("/")],
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/company", () => ({
  companyFloorsOptions: () => ({ queryKey: ["floors"] }),
  useDeleteCompanyFloor: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateCompanyFloor: () => ({ mutate: vi.fn(), isPending: false }),
  useCreateCompanyFloor: () => ({ mutate: vi.fn(), isPending: false }),
  useSetCompanySeats: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
  workspaceKeys: { squads: () => ["squads"], agents: () => ["agents"] },
}));
vi.mock("../lib/use-company-admin", () => ({ useCompanyAdmin: () => false }));
vi.mock("../../issues/surface/peek-context", () => ({ useIssuePeekActions: () => null }));
vi.mock("../../modals/create-squad", () => ({ CreateSquadModal: () => null }));
// The detail panes have their own tests; this one checks the drill-down wiring.
vi.mock("./company-squad-detail", () => ({
  CompanySquadDetail: ({ squad }: { squad: { name: string } }) => <div>squad pane: {squad.name}</div>,
}));
vi.mock("./company-agent-detail", () => ({
  CompanyAgentDetail: ({ agentId }: { agentId: string }) => <div>agent pane: {agentId}</div>,
}));

function seed() {
  mocks.queries = {
    floors: {
      data: [
        { id: "f-2", name: "Product", position: 1 },
        { id: "f-1", name: "Executive", position: 0 },
      ],
    },
    squads: {
      data: [
        { id: "s-1", name: "Planning", floor_id: "f-1", leader_id: "a-1", member_count: 2 },
        { id: "s-2", name: "Discovery", floor_id: "f-2", leader_id: "a-2", member_count: 3 },
        { id: "s-3", name: "Loose squad", floor_id: null, leader_id: "a-1", member_count: 1 },
      ],
    },
    agents: {
      data: [
        { id: "a-1", name: "Secretary", skills: [], instructions: "" },
        { id: "a-2", name: "Researcher", skills: [], instructions: "" },
      ],
    },
  };
}

function renderOverview(search = "") {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/company",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => `https://multica.test${path}`,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <CompanyOverview selection={readCompanySelection(adapter.searchParams)} />
    </NavigationProvider>,
  );
  return adapter;
}

beforeEach(() => {
  seed();
});

describe("CompanyOverview drill-down", () => {
  it("lists departments in floor order, with unseated squads last", () => {
    renderOverview();
    const departments = screen.getByRole("complementary", { name: "Departments" });
    const names = Array.from(departments.querySelectorAll("button")).map((b) => b.textContent ?? "");
    expect(names[0]).toContain("Executive");
    expect(names[1]).toContain("Product");
    expect(names[2]).toContain("Not in a department");
  });

  it("opens a department in the URL without leaving the page", async () => {
    const adapter = renderOverview();
    await userEvent.click(screen.getByRole("button", { name: /Executive/ }));
    expect(adapter.push).toHaveBeenCalledWith("/acme/company?dept=f-1");
  });

  it("opens a squad, which also names its department", async () => {
    const adapter = renderOverview("dept=f-1");
    await userEvent.click(screen.getByRole("button", { name: /Planning/ }));
    expect(adapter.push).toHaveBeenCalledWith("/acme/company?dept=f-1&squad=s-1");
  });

  it("shows the squad pane for a squad in the URL and steps back through the breadcrumb", async () => {
    const adapter = renderOverview("dept=f-1&squad=s-1");
    expect(screen.getByText("squad pane: Planning")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Executive" }));
    expect(adapter.push).toHaveBeenCalledWith("/acme/company?dept=f-1");
  });

  it("derives the department from a squad named alone in the URL", () => {
    renderOverview("squad=s-2");
    expect(screen.getByText("squad pane: Discovery")).toBeInTheDocument();
  });
});

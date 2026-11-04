/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { companySetupOptions, secretaryDeskDirectivesOptions, secretaryDeskRequestsOptions } from "@multica/core/company";
import { squadListOptions } from "@multica/core/workspace/queries";
import type { Issue, Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SecretaryDeskDialog } from "./secretary-desk-dialog";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

// Status keys stand in for labels; the catalog is covered by its own tests.
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => status,
}));

function issue(patch: Record<string, unknown> & { id: string; title: string }): Issue {
  return {
    identifier: `MUL-${patch.id}`,
    number: 1,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "agent",
    creator_id: "sec-1",
    parent_issue_id: null,
    project_id: null,
    metadata: {},
    created_at: "2026-11-03T00:00:00Z",
    ...patch,
  } as unknown as Issue;
}

const SQUAD: Squad = { id: "squad-1", name: "Pricing squad" } as unknown as Squad;

function renderDesk({
  directives,
  requests,
}: {
  directives: Issue[];
  requests: Issue[];
}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(companySetupOptions("ws-1").queryKey, {
    runtime_online: true,
    secretary_id: "sec-1",
    secretary_desk_project_id: "desk-1",
    executive_floor: true,
    autopilot_daily: true,
    autopilot_weekly_paused: true,
    ready: true,
  });
  qc.setQueryData(secretaryDeskDirectivesOptions("ws-1").queryKey, directives);
  qc.setQueryData(secretaryDeskRequestsOptions("ws-1", "sec-1").queryKey, requests);
  qc.setQueryData(squadListOptions("ws-1").queryKey, [SQUAD]);
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/company",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => `https://multica.test${path}`,
  };
  return renderWithI18n(
    <NavigationProvider value={navigation}>
      <QueryClientProvider client={qc}>
        <SecretaryDeskDialog open onClose={() => {}} />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

afterEach(() => {
  cleanup();
});

describe("SecretaryDeskDialog", () => {
  it("shows a directive with the boss's quote, its squad and status", () => {
    renderDesk({
      directives: [
        issue({
          id: "1",
          title: "Cut the onboarding to three steps",
          assignee_type: "squad",
          assignee_id: "squad-1",
          metadata: { directive: { quote: "make onboarding shorter" } },
        }),
      ],
      requests: [],
    });

    const row = screen.getByRole("link", { name: /Cut the onboarding/ }).closest("li") as HTMLElement;
    expect(within(row).getByText("make onboarding shorter")).toBeInTheDocument();
    expect(within(row).getByText("Pricing squad")).toBeInTheDocument();
    expect(within(row).getByText("todo")).toBeInTheDocument();
  });

  it("lists the squad requests and directives, newest first", () => {
    renderDesk({
      directives: [issue({ id: "1", title: "Older directive", created_at: "2026-11-01T00:00:00Z" })],
      requests: [issue({ id: "2", title: "Check the margin model", created_at: "2026-11-02T00:00:00Z" })],
    });

    const links = screen.getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual(
      expect.arrayContaining([expect.stringContaining("Check the margin model"), expect.stringContaining("Older directive")]),
    );
    expect(links[0]?.textContent).toContain("Check the margin model");
  });

  it("shows the empty state when there is nothing on the desk", () => {
    renderDesk({ directives: [], requests: [] });
    expect(screen.getByText("Nothing on the desk yet")).toBeInTheDocument();
  });
});

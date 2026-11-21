import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueViewPreference } from "@multica/core/api/schemas";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { renderWithI18n } from "../../test/i18n";
import { ProjectsSectionSettings } from "./sidebar-settings";

const server = vi.hoisted(() => ({
  doc: null as unknown as IssueViewPreference,
  getIssueViewPreference: vi.fn(),
  putIssueViewPreference: vi.fn(),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    getIssueViewPreference: server.getIssueViewPreference,
    putIssueViewPreference: server.putIssueViewPreference,
  },
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () =>
    buildIssueStatusCatalog(
      [
        ["todo", "Todo", "unstarted"],
        ["in_progress", "In Progress", "started"],
        ["done", "Done", "done"],
      ].map(([key, name, category], position) => ({
        id: key!,
        workspace_id: "ws-1",
        key: key!,
        name: name!,
        description: "",
        category: category as "unstarted",
        color: "",
        is_system: true,
        position,
        archived_at: null,
        created_at: "",
        updated_at: "",
      })),
    ),
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));

const saved = (prefs: Record<string, unknown>): IssueViewPreference => ({
  scope_type: "sidebar_projects",
  scope_id: "u1",
  updated_at: "2026-10-31T00:00:00Z",
  prefs: { hidden: [], order: [], ...prefs },
});

let qc: QueryClient;

beforeEach(() => {
  server.doc = saved({});
  server.getIssueViewPreference.mockReset().mockImplementation(async () => server.doc);
  server.putIssueViewPreference
    .mockReset()
    .mockImplementation(async ({ prefs }: { prefs: Record<string, unknown> }) => (server.doc = saved(prefs)));
  qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey, server.doc);
});

function openSettings() {
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectsSectionSettings wsId="ws-1" />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Project display" }));
}

const chip = (name: string) => screen.getByRole("button", { name });
const lastSaved = () => server.putIssueViewPreference.mock.lastCall![0].prefs;

describe("ProjectsSectionSettings", () => {
  it("covers both the project list and every project's issues", () => {
    openSettings();

    expect(screen.getByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Issues in projects" })).toBeInTheDocument();
  });

  it("saves issue statuses toggled from their chips for every device", async () => {
    openSettings();

    expect(chip("Todo")).toHaveAttribute("aria-pressed", "true");
    expect(chip("Done")).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(chip("Todo"));
    await waitFor(() => expect(chip("Todo")).toHaveAttribute("aria-pressed", "false"));
    expect(lastSaved().issues.hiddenStatuses).toEqual(expect.arrayContaining(["todo", "done"]));
  });

  it("saves me and my agents and keeps it once the saved document is read back", async () => {
    const user = userEvent.setup();
    openSettings();

    const assignee = screen.getByRole("combobox", { name: "Assignee" });
    expect(assignee).toHaveTextContent("Anyone");
    await user.click(assignee);
    await user.click(await screen.findByRole("option", { name: "Me and my agents" }));

    await waitFor(() => expect(server.putIssueViewPreference).toHaveBeenCalled());
    expect(lastSaved().issues.assignee).toBe("me_and_my_agents");
    await waitFor(() => expect(server.doc.prefs.issues).toMatchObject({ assignee: "me_and_my_agents" }));
    await act(() => qc.refetchQueries());
    expect(server.getIssueViewPreference).toHaveBeenCalled();
    expect(assignee).toHaveTextContent("Me and my agents");
  });

  it("toggles a project status and resets everything at once", async () => {
    openSettings();

    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull();
    fireEvent.click(chip("Completed"));
    await waitFor(() => expect(chip("Completed")).toHaveAttribute("aria-pressed", "true"));
    fireEvent.click(chip("Todo"));
    await waitFor(() => expect(chip("Todo")).toHaveAttribute("aria-pressed", "false"));
    await waitFor(() => expect(server.putIssueViewPreference).toHaveBeenCalledTimes(2));
    expect(lastSaved().projects.statuses).toContain("completed");

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Reset" })).toBeNull());
    // Writes go out one at a time; the reset is the last to be sent.
    await waitFor(() => expect(lastSaved().projects).toEqual(DEFAULT_SIDEBAR_PROJECT_SETTINGS));
    expect(lastSaved().issues).toEqual(DEFAULT_SIDEBAR_ISSUE_SETTINGS);
  });
});

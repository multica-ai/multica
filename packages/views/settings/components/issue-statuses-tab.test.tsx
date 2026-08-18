/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { IssueStatusEntry } from "@multica/core/types";
import en from "../../locales/en/settings.json";
import { IssueStatusesTab } from "./issue-statuses-tab";

const reorderMutate = vi.hoisted(() => vi.fn());
let catalog: IssueStatusEntry[] = [];
let role: string = "owner";
let flagOn = true;

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => ({
    data: options.queryKey[0] === "issue-statuses" ? catalog : members(),
    isLoading: false,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) => selector({ user: { id: "u-1" } }),
}));
vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => flagOn }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "ws-1"] }),
}));
vi.mock("@multica/core/issue-statuses/queries", () => ({
  issueStatusListOptions: () => ({ queryKey: ["issue-statuses", "ws-1"] }),
}));
vi.mock("@multica/core/issue-statuses/mutations", () => ({
  useCreateIssueStatus: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateIssueStatus: () => ({ mutate: vi.fn(), isPending: false }),
  useArchiveIssueStatus: () => ({ mutate: vi.fn() }),
  useReorderIssueStatuses: () => ({ mutate: reorderMutate }),
}));
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (key: string) => key,
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, unknown>) => {
      const template = accessor(en);
      if (!params) return template;
      return template.replace(/\{\{(\w+)\}\}/g, (_, k: string) => String(params[k] ?? ""));
    },
  }),
}));

function members() {
  return [{ user_id: "u-1", role }];
}

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "ws-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "in_review",
    color: "#ff0000",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const BUILT_IN_IN_REVIEW = entry({
  id: "in_review",
  key: "in_review",
  name: "In Review",
  is_system: true,
  position: 0,
});

afterEach(() => {
  cleanup();
  reorderMutate.mockClear();
  catalog = [];
  role = "owner";
  flagOn = true;
});

describe("IssueStatusesTab", () => {
  it("offers status creation to an owner when the rollout flag is on", () => {
    catalog = [BUILT_IN_IN_REVIEW];
    render(<IssueStatusesTab />);

    expect(screen.getAllByText(en.issue_statuses.add).length).toBeGreaterThan(0);
    expect(screen.queryByText(en.issue_statuses.flag_off)).toBeNull();
  });

  // The flag gates CREATION only. Existing statuses stay fully readable, which
  // is what makes the flag safe to leave off after the backend ships.
  it("hides creation and explains why when the flag is off", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    flagOn = false;
    render(<IssueStatusesTab />);

    expect(screen.queryByText(en.issue_statuses.add)).toBeNull();
    expect(screen.getByText(en.issue_statuses.flag_off)).toBeInTheDocument();
    expect(screen.getByText("QA")).toBeInTheDocument();
  });

  it("hides creation and row actions from a non-admin member", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    role = "member";
    render(<IssueStatusesTab />);

    expect(screen.queryByText(en.issue_statuses.add)).toBeNull();
    expect(screen.getByText("QA")).toBeInTheDocument();
    expect(
      screen.queryByLabelText(
        en.issue_statuses.actions.open.replace("{{name}}", "QA"),
      ),
    ).toBeNull();
  });

  // Archiving retires a status from FUTURE assignment; the row has to stay
  // legible so an admin can tell what a lingering status on an old issue is.
  it("keeps an archived status visible but not actionable", () => {
    catalog = [
      BUILT_IN_IN_REVIEW,
      entry({ key: "qa", name: "QA", archived_at: "2026-01-01T00:00:00Z" }),
    ];
    render(<IssueStatusesTab />);

    // Hidden until the toggle is on, but the toggle itself is enabled because
    // the workspace has one.
    expect(screen.queryByText("QA")).toBeNull();
    expect(screen.getByRole("switch")).toBeEnabled();
  });

  it("does not offer reorder when a category holds a single custom status", () => {
    catalog = [BUILT_IN_IN_REVIEW, entry({ key: "qa", name: "QA" })];
    render(<IssueStatusesTab />);

    expect(
      screen.queryByLabelText(
        en.issue_statuses.actions.reorder.replace("{{name}}", "QA"),
      ),
    ).toBeNull();
  });

  it("offers reorder once a category holds two", () => {
    catalog = [
      BUILT_IN_IN_REVIEW,
      entry({ id: "qa", key: "qa", name: "QA", position: 1 }),
      entry({ id: "uat", key: "uat", name: "UAT", position: 2 }),
    ];
    render(<IssueStatusesTab />);

    expect(
      screen.getByLabelText(en.issue_statuses.actions.reorder.replace("{{name}}", "QA")),
    ).toBeInTheDocument();
  });
});

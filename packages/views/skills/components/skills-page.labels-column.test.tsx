// @vitest-environment jsdom

import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import type { Label, SkillSummary } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";


const mocks = vi.hoisted(() => ({
  skills: [] as SkillSummary[],
  viewState: {
    sortField: "name",
    sortDirection: "asc" as string,
    hiddenColumns: [] as string[],
    search: "",
    setSearch: vi.fn(),
    filters: {
      usage: [] as string[],
      origins: [] as string[],
      agents: [] as string[],
      creators: [] as string[],
      labels: [] as string[],
    },
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    if (options.queryKey?.[0] === "skills") {
      return {
        data: mocks.skills,
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      };
    }
    return { data: [], isLoading: false, error: null, refetch: vi.fn() };
  },
}));

// Render every row so each row's cells are mounted.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 48,
        end: (index + 1) * 48,
        size: 48,
      })),
    getTotalSize: () => count * 48,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Partial mock: SkillIcon resolves its icon from the real WORKSPACE_PAGES.
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    skillDetail: (id: string) => `/acme/skills/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  skillListOptions: () => ({ queryKey: ["skills"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  selectSkillAssignments: () => new Map(),
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeDisplayLabel: () => "runtime",
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (u: string | null) => u,
}));

vi.mock("@multica/core/skills/stores", () => ({
  useSkillsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
  DEFAULT_HIDDEN_COLUMNS: [],
}));

// View-layer children with heavy / portal deps — stubbed to keep the test on
// the table's cells.
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: () => null,
}));
vi.mock("./create-skill-dialog", () => ({ CreateSkillDialog: () => null }));
vi.mock("./skill-list-controls", () => ({
  countActiveFilterDimensions: () => 0,
  SkillFilterMenu: () => null,
  SkillDisplayMenu: () => null,
  SkillCreateMenu: () => null,
}));
vi.mock("./skill-list-actions", () => ({
  SkillBatchToolbar: () => null,
  SkillRowActions: () => null,
}));

import SkillsPage from "./skills-page";

function label(id: string, name: string): Label {
  return {
    id,
    workspace_id: "ws-1",
    name,
    color: "#3b82f6",
    created_at: "2026-07-28T18:11:37Z",
    updated_at: "2026-07-28T18:11:37Z",
  };
}

function skill(id: string, name: string, labels?: Label[]): SkillSummary {
  return {
    id,
    workspace_id: "ws-1",
    name,
    description: "",
    config: {},
    created_by: "user-1",
    created_at: "2026-07-28T18:11:37Z",
    updated_at: "2026-07-28T18:14:40Z",
    ...(labels ? { labels } : {}),
  };
}

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/skills",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p) => p,
  openInNewTab: vi.fn(),
};

function renderPage() {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SkillsPage />
    </NavigationProvider>,
  );
}

function rowOf(name: string): HTMLElement {
  const row = screen.getByText(name).closest("[role='row'], tr, [data-index]");
  if (!(row instanceof HTMLElement)) throw new Error(`no row for ${name}`);
  return row;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.viewState.hiddenColumns = [];
});

describe("SkillsPage labels column", () => {
  it("shows each skill's labels in its row", async () => {
    mocks.skills = [
      skill("s1", "triage", [label("l1", "Incident diagnosis")]),
      skill("s2", "writer", [label("l2", "Communication")]),
    ];
    renderPage();

    await screen.findByText("triage");
    expect(screen.getByText("Labels")).toBeTruthy();
    expect(within(rowOf("triage")).getByText("Incident diagnosis")).toBeTruthy();
    expect(within(rowOf("writer")).getByText("Communication")).toBeTruthy();
    expect(within(rowOf("triage")).queryByText("Communication")).toBeNull();
  });

  it("shows two labels and counts the rest", async () => {
    mocks.skills = [
      skill("s1", "triage", [
        label("l1", "Core"),
        label("l2", "Workflows"),
        label("l3", "Review"),
        label("l4", "Other"),
      ]),
    ];
    renderPage();

    await screen.findByText("triage");
    const row = rowOf("triage");
    expect(within(row).getByText("Core")).toBeTruthy();
    expect(within(row).getByText("Workflows")).toBeTruthy();
    expect(within(row).queryByText("Review")).toBeNull();
    expect(within(row).getByText("+2")).toBeTruthy();
  });

  it("drops the column when the user hides it", async () => {
    mocks.viewState.hiddenColumns = ["labels"];
    mocks.skills = [skill("s1", "triage", [label("l1", "Incident diagnosis")])];
    renderPage();

    await screen.findByText("triage");
    expect(screen.queryByText("Labels")).toBeNull();
    expect(screen.queryByText("Incident diagnosis")).toBeNull();
  });
});

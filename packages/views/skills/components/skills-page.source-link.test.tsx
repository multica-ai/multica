// @vitest-environment jsdom

import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { SkillSummary } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// Regression rig for the Source cell's anchor living inside a `useRowLink`
// row: the row handles BOTH click and auxclick, so the anchor must stop both
// from bubbling. Stopping only click let a middle click reach the row, whose
// preventDefault() cancelled the anchor's native open — the skill detail page
// opened in a background tab instead of the source page.

const mocks = vi.hoisted(() => ({
  skills: [] as SkillSummary[],
  viewState: {
    sortField: "name",
    sortDirection: "asc" as string,
    hiddenColumns: [] as string[],
    filters: {
      usage: [] as string[],
      origins: [] as string[],
      agents: [] as string[],
      creators: [] as string[],
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

// Render every row so the anchor under test is always mounted.
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
// the row/anchor event wiring.
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: () => null,
}));
vi.mock("./create-skill-dialog", () => ({ CreateSkillDialog: () => null }));
vi.mock("./skill-list-toolbar", () => ({ SkillListToolbar: () => null }));
vi.mock("./skill-list-actions", () => ({
  SkillBatchToolbar: () => null,
  SkillRowActions: () => null,
}));

import SkillsPage from "./skills-page";

const SOURCE_URL = "https://github.com/anthropics/skills/tree/main/animations";

const importedSkill: SkillSummary = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "animations",
  description: "",
  config: { origin: { type: "github", source_url: SOURCE_URL } },
  created_by: "user-1",
  created_at: "2026-07-28T18:11:37Z",
  updated_at: "2026-07-28T18:14:40Z",
};

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/skills",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    openInNewTab: vi.fn(),
    ...overrides,
  };
}

function renderPage(adapter: NavigationAdapter) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SkillsPage />
    </NavigationProvider>,
  );
}

function middleClick(target: Element): MouseEvent {
  const event = new MouseEvent("auxclick", {
    bubbles: true,
    button: 1,
    cancelable: true,
  });
  target.dispatchEvent(event);
  return event;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.skills = [importedSkill];
});

describe("SkillsPage source link vs row navigation", () => {
  it("middle click on the source link keeps its native open (row must not hijack)", async () => {
    const adapter = makeAdapter();
    renderPage(adapter);

    const link = await screen.findByRole("link", { name: "From GitHub" });
    expect(link.getAttribute("href")).toBe(SOURCE_URL);

    const event = middleClick(link);

    // The anchor's native middle-click open must survive: the row's auxclick
    // handler (which calls preventDefault) must never see the event.
    expect(event.defaultPrevented).toBe(false);
    expect(adapter.openInNewTab).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("left click on the source link does not trigger row navigation", async () => {
    const adapter = makeAdapter();
    renderPage(adapter);

    fireEvent.click(
      await screen.findByRole("link", { name: "From GitHub" }),
    );

    expect(adapter.push).not.toHaveBeenCalled();
    expect(adapter.openInNewTab).not.toHaveBeenCalled();
  });

  // Sanity check that the rig exercises the row's auxclick handler at all —
  // without this, the assertions above could pass vacuously.
  it("middle click elsewhere on the row still background-tabs the skill", async () => {
    const adapter = makeAdapter();
    renderPage(adapter);

    const event = middleClick(await screen.findByText("animations"));

    expect(event.defaultPrevented).toBe(true);
    expect(adapter.openInNewTab).toHaveBeenCalledWith(
      "/acme/skills/skill-1",
      "animations",
    );
  });
});

// @vitest-environment jsdom

import React, { useMemo, useState } from "react";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SupportedLocale } from "@multica/core/i18n";
import type { SkillSummary } from "@multica/core/types";
import type { SkillFolderTree } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const apiMock = vi.hoisted(() => ({
  createSkillFolder: vi.fn(),
  updateSkillFolder: vi.fn(),
  deleteSkillFolder: vi.fn(),
  detachSkillPlacement: vi.fn(),
}));

const mocks = vi.hoisted(() => ({
  skills: [] as SkillSummary[],
  folderTree: null as SkillFolderTree | null | undefined,
  folderTreeError: false,
  folderTreeFailures: 0,
  refetchTree: vi.fn(),
  currentUserId: "user-1",
  members: [] as { user_id: string; role: string }[],
  virtualizerCount: 0,
  virtualizerKeys: [] as (string | number)[],
  viewState: {
    sortField: "name",
    sortDirection: "asc" as string,
    hiddenColumns: [] as string[],
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
    const key = options.queryKey ?? [];
    if (key[0] === "skills") {
      return { data: mocks.skills, isLoading: false, error: null, refetch: vi.fn() };
    }
    if (key.includes("skill-folders")) {
      return {
        data: mocks.folderTree,
        isLoading: false,
        error: mocks.folderTreeError ? new Error("tree unavailable") : null,
        failureCount: mocks.folderTreeFailures,
        refetch: mocks.refetchTree,
      };
    }
    if (key[0] === "members") {
      return { data: mocks.members, isLoading: false, error: null, refetch: vi.fn() };
    }
    return { data: [], isLoading: false, error: null, refetch: vi.fn() };
  },
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    setQueryData: vi.fn(),
  }),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({
    count,
    getItemKey,
  }: {
    count: number;
    getItemKey: (index: number) => string | number;
  }) => {
    mocks.virtualizerCount = count;
    mocks.virtualizerKeys = Array.from({ length: count }, (_, index) => getItemKey(index));
    return {
      getVirtualItems: () =>
        Array.from({ length: count }, (_, index) => ({
          index,
          key: getItemKey(index),
          start: index * 48,
          end: (index + 1) * 48,
          size: 48,
        })),
      getTotalSize: () => count * 48,
    };
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: mocks.currentUserId } }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  // Keep the real ApiError / errorCode so the dialogs' coded-error mapping
  // runs against the production helper; only the api client is stubbed.
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    skills: () => "/acme/skills",
    skillDetail: (id: string) => `/acme/skills/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  skillListOptions: () => ({ queryKey: ["skills"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  selectSkillAssignments: () => new Map(),
}));
vi.mock("@multica/core/skills/package-queries", () => ({
  skillFolderTreeOptions: () => ({ queryKey: ["workspaces", "ws-1", "skill-folders"] }),
  invalidateSkillPackageQueries: vi.fn(),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeDisplayLabel: () => "runtime",
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null) => url,
}));
vi.mock("@multica/core/skills/stores", () => ({
  useSkillsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
  DEFAULT_HIDDEN_COLUMNS: [],
}));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: (props: Record<string, unknown>) => (
    <div data-testid="skeleton" {...props} />
  ),
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: () => null,
}));
vi.mock("./create-skill-dialog", () => ({ CreateSkillDialog: () => null }));
vi.mock("./skill-list-toolbar", () => ({
  SkillListToolbar: ({
    search,
    onSearchChange,
    visibleCount,
    countLabel,
    folderActions,
  }: {
    search: string;
    onSearchChange: (value: string) => void;
    visibleCount: number;
    countLabel?: string;
    folderActions?: React.ReactNode;
  }) => (
    <div>
      <input
        aria-label="Search skills"
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
      />
      <span data-testid="visible-count">{visibleCount}</span>
      {countLabel && <span>{countLabel}</span>}
      {folderActions}
    </div>
  ),
}));
vi.mock("./skill-list-actions", () => ({
  SkillBatchToolbar: ({ rows }: { rows: { skill: { id: string } }[] }) => (
    <div data-testid="batch-selection">{rows.map((row) => row.skill.id).join(",")}</div>
  ),
  SkillRowActions: () => null,
}));
vi.mock("./package-import-dialog", () => ({ ImportPackageDialog: () => null }));
vi.mock("./package-rescan-dialog", () => ({
  RescanPackageDialog: () => <div data-testid="rescan-dialog" />,
}));
vi.mock("./package-remove-dialog", () => ({
  PackageRemoveDialog: ({ mode }: { mode: string }) => (
    <div data-testid={`remove-dialog-${mode}`} />
  ),
}));
vi.mock("./move-skills-dialog", () => ({ MoveSkillsDialog: () => null }));

import SkillsPage from "./skills-page";

function makeSkill(id: string, name: string): SkillSummary {
  return {
    id,
    workspace_id: "ws-1",
    name,
    description: "",
    config: {},
    created_by: "user-1",
    created_at: "2026-07-28T18:11:37Z",
    updated_at: "2026-07-28T18:14:40Z",
  };
}

const folderTree: SkillFolderTree = {
  folders: [
    {
      id: "f-eng",
      workspace_id: "ws-1",
      parent_id: null,
      name: "Engineering",
      package_id: "p1",
      package_path: "",
      created_at: "",
      updated_at: "",
    },
    {
      id: "f-eng-fe",
      workspace_id: "ws-1",
      parent_id: "f-eng",
      name: "Frontend",
      package_id: "p1",
      package_path: "frontend",
      created_at: "",
      updated_at: "",
    },
    {
      id: "f-design",
      workspace_id: "ws-1",
      parent_id: null,
      name: "Design",
      package_id: null,
      package_path: null,
      created_at: "",
      updated_at: "",
    },
    {
      id: "f-empty",
      workspace_id: "ws-1",
      parent_id: null,
      name: "Empty",
      package_id: null,
      package_path: null,
      created_at: "",
      updated_at: "",
    },
  ],
  placements: [
    { workspace_id: "ws-1", skill_id: "placed", folder_id: "f-eng-fe", package_id: "p1", source_path: "skills/placed" },
    { workspace_id: "ws-1", skill_id: "engineering", folder_id: "f-eng", package_id: "p1", source_path: "skills/engineering" },
    { workspace_id: "ws-1", skill_id: "design", folder_id: "f-design", package_id: null, source_path: null },
  ],
  packages: [
    {
      id: "p1",
      workspace_id: "ws-1",
      owner_repo: "o/r",
      subdirectory: "",
      source_url: "https://github.com/o/r/tree/main",
      ref: "main",
      root_folder_id: "f-eng",
      created_by: "user-1",
      revision: 1,
      candidates: [
        { path: "skills/placed", name: "placed", description: "" },
        { path: "skills/engineering", name: "engineering", description: "" },
        { path: "skills/frontend/gray", name: "gray-skill", description: "" },
      ],
    },
  ],
};

type NavController = {
  path: string;
  push: Mock<(path: string) => void>;
  replace: Mock<(path: string) => void>;
  back: Mock<() => void>;
};

function makeController(path: string): NavController {
  return {
    path,
    push: vi.fn<(path: string) => void>(),
    replace: vi.fn<(path: string) => void>(),
    back: vi.fn<() => void>(),
  };
}

function NavigationHarness({
  controller,
  initialPath,
  children,
}: {
  controller: NavController;
  initialPath: string;
  children: React.ReactNode;
}) {
  const [history, setHistory] = useState({ entries: [initialPath], index: 0 });
  const currentPath = history.entries[history.index]!;
  controller.path = currentPath;
  useMemo(() => {
    controller.push.mockImplementation((path: string) => {
      setHistory((previous) => ({
        entries: [...previous.entries.slice(0, previous.index + 1), path],
        index: previous.index + 1,
      }));
    });
    controller.replace.mockImplementation((path: string) => {
      setHistory((previous) => ({
        ...previous,
        entries: previous.entries.map((entry, index) =>
          index === previous.index ? path : entry,
        ),
      }));
    });
    controller.back.mockImplementation(() => {
      setHistory((previous) => ({
        ...previous,
        index: Math.max(0, previous.index - 1),
      }));
    });
  }, [controller]);
  const url = new URL(currentPath, "https://example.test");
  const adapter: NavigationAdapter = {
    push: controller.push,
    replace: controller.replace,
    back: controller.back,
    pathname: url.pathname,
    searchParams: new URLSearchParams(url.search),
    hash: url.hash,
    getShareableUrl: (path) => path,
    openInNewTab: vi.fn(),
  };
  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

function renderPage(initialPath = "/acme/skills", locale?: SupportedLocale) {
  const controller = makeController(initialPath);
  const ui = (
    <NavigationHarness controller={controller} initialPath={initialPath}>
      <SkillsPage />
    </NavigationHarness>
  );
  const result = renderWithI18n(ui, { locale });
  return { ...result, controller };
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  mocks.skills = [
    makeSkill("placed", "placed-skill"),
    makeSkill("engineering", "engineering-skill"),
    makeSkill("design", "design-skill"),
    makeSkill("loose", "loose-skill"),
  ];
  mocks.folderTree = folderTree;
  mocks.folderTreeError = false;
  mocks.folderTreeFailures = 0;
  mocks.refetchTree = vi.fn();
  mocks.currentUserId = "user-1";
  mocks.members = [];
  mocks.virtualizerCount = 0;
  mocks.virtualizerKeys = [];
  for (const key of Object.keys(mocks.viewState.filters) as (keyof typeof mocks.viewState.filters)[]) {
    mocks.viewState.filters[key] = [];
  }
  apiMock.createSkillFolder.mockResolvedValue({ id: "new-folder" });
  apiMock.updateSkillFolder.mockResolvedValue({ id: "folder" });
  apiMock.deleteSkillFolder.mockResolvedValue({ deleted: true });
  apiMock.detachSkillPlacement.mockResolvedValue({ updated: true });
});

describe("SkillsPage folder hierarchy", () => {
  it("starts with every folder collapsed and only root rows visible", async () => {
    renderPage();
    expect(await screen.findByText("loose-skill")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Empty" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Expand Frontend" })).toBeNull();
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(screen.queryByText("engineering-skill")).toBeNull();
    expect(screen.queryByText("design-skill")).toBeNull();
    expect(screen.queryByText("gray-skill")).toBeNull();
    expect(screen.getByTestId("visible-count").textContent).toBe("4");
  });

  it("expands direct children in place and collapses the whole subtree without routing", async () => {
    const { controller } = renderPage("/acme/skills?view=compact");
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    expect(await screen.findByText("engineering-skill")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Frontend" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(controller.path).toBe("/acme/skills?view=compact");

    await userEvent.click(screen.getByRole("button", { name: "Expand Frontend" }));
    expect(await screen.findByText("placed-skill")).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Collapse Engineering" }));
    expect(screen.queryByRole("button", { name: "Expand Frontend" })).toBeNull();
    expect(screen.queryByText("engineering-skill")).toBeNull();
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(screen.queryByText("gray-skill")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(controller.path).toBe("/acme/skills?view=compact");
    expect(controller.push).not.toHaveBeenCalled();
    expect(controller.replace).not.toHaveBeenCalled();
    expect(controller.back).not.toHaveBeenCalled();
  });

  it("keeps the root list intact when an empty folder is expanded", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Empty" }));
    expect(await screen.findByRole("button", { name: "Collapse Empty" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(screen.queryByText("This folder is empty.")).toBeNull();
    expect(screen.getByTestId("visible-count").textContent).toBe("4");
  });

  it("navigates to skill details without a folder query", async () => {
    const { controller } = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Design" }));
    await userEvent.click(await screen.findByText("design-skill"));
    expect(controller.path).toBe("/acme/skills/design");
  });
});

describe("SkillsPage expansion persistence", () => {
  it("persists expansion under the workspace key and restores it after a remount", async () => {
    const storageKey = "multica.folder_explorer.expanded.skills.ws-1";
    const { unmount } = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    expect(JSON.parse(window.localStorage.getItem(storageKey)!)).toEqual(["f-eng"]);

    unmount();
    renderPage();
    expect(await screen.findByText("engineering-skill")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Frontend" })).toBeTruthy();
    expect(screen.queryByText("placed-skill")).toBeNull();
  });

  it("expands and collapses every folder from the toolbar actions", async () => {
    const storageKey = "multica.folder_explorer.expanded.skills.ws-1";
    renderPage();
    expect(await screen.findByRole("button", { name: "Expand all" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse all" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New folder" })).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Expand all" }));
    expect(await screen.findByText("placed-skill")).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("design-skill")).toBeTruthy();
    expect(screen.getByText("gray-skill")).toBeTruthy();
    expect(JSON.parse(window.localStorage.getItem(storageKey)!)).toEqual([
      "f-eng",
      "f-eng-fe",
      "f-design",
      "f-empty",
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(screen.queryByText("engineering-skill")).toBeNull();
    expect(screen.queryByText("design-skill")).toBeNull();
    expect(screen.queryByText("gray-skill")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Empty" })).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(window.localStorage.getItem(storageKey)).toBe("[]");
  });
});

describe("SkillsPage folder actions", () => {
  it("creates a subfolder from a custom folder menu and expands its parent", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("New subfolder"));
    await userEvent.type(screen.getByLabelText("Name"), "Research");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() =>
      expect(apiMock.createSkillFolder).toHaveBeenCalledWith("ws-1", {
        name: "Research",
        parent_id: "f-design",
      }),
    );
    await waitFor(() =>
      expect(
        window.localStorage.getItem("multica.folder_explorer.expanded.skills.ws-1"),
      ).toBe('["f-design"]'),
    );
  });

  it("creates a root folder from the toolbar actions", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Research");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() =>
      expect(apiMock.createSkillFolder).toHaveBeenCalledWith("ws-1", {
        name: "Research",
        parent_id: "",
      }),
    );
  });

  it("does not offer subfolder creation for managed package folders", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Actions for Engineering" }));
    expect(await screen.findByText("Rescan")).toBeTruthy();
    expect(screen.queryByText("New subfolder")).toBeNull();
    expect(screen.queryByRole("button", { name: "Actions for Frontend" })).toBeNull();
    expect(screen.getByRole("button", { name: "New folder" }).hasAttribute("disabled")).toBe(false);
  });

  it("keeps custom and package-root menus, including package dialogs", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    expect(await screen.findByText("Rename")).toBeTruthy();
    expect(screen.getByText("Move")).toBeTruthy();
    expect(screen.getByText("Delete")).toBeTruthy();
    expect(screen.getByText("New subfolder")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Actions for Engineering" }));
    await userEvent.click(await screen.findByText("Rescan"));
    expect(await screen.findByTestId("rescan-dialog")).toBeTruthy();
  });

  it("explains that deleting a folder moves contents up without deleting skills", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    expect(
      await screen.findByText("Its skills and subfolders move up one level. No skills are deleted.", { exact: false }),
    ).toBeTruthy();
  });
});

describe("SkillsPage package candidates", () => {
  it("lists not-imported skills after imported skills in the same expanded folder", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Expand Frontend" }));
    const gray = await screen.findByText("gray-skill");
    const grayRow = gray.closest("[aria-disabled='true']");
    expect(grayRow).toBeTruthy();
    expect(screen.getByText("Not imported")).toBeTruthy();
    expect(gray.closest("[data-row-link]")).toBeNull();
    const placed = screen.getByText("placed-skill").closest("[role='row']");
    expect(placed).toBeTruthy();
    expect(placed!.compareDocumentPosition(grayRow!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Frontend" })).toBeTruthy();
  });
});

describe("SkillsPage flat view and tree fallback", () => {
  it("searches every folder with path hints and restores the expanded tree when cleared", async () => {
    const { controller } = renderPage("/acme/skills?view=compact");
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Expand Frontend" }));
    const search = screen.getByRole("textbox", { name: "Search skills" });
    await userEvent.type(search, "skill");
    expect(await screen.findByText("Results from all folders")).toBeTruthy();
    expect(screen.getByText("Engineering / Frontend")).toBeTruthy();
    expect(screen.getByText("Design")).toBeTruthy();
    expect(screen.getByText("design-skill")).toBeTruthy();
    expect(screen.getByText("loose-skill").parentElement?.textContent).toBe("loose-skill");
    expect(screen.queryByRole("button", { name: "Expand Design" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand all" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Collapse all" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New folder" })).toBeNull();
    expect(screen.queryByText("gray-skill")).toBeNull();
    expect(controller.path).toBe("/acme/skills?view=compact");

    await userEvent.clear(search);
    expect(await screen.findByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("placed-skill")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Frontend" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.queryByText("design-skill")).toBeNull();
    expect(controller.path).toBe("/acme/skills?view=compact");
  });

  it("passes the filtered skill row count to the toolbar instead of the explorer row count", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    expect(screen.getByTestId("visible-count").textContent).toBe("4");
    expect(mocks.virtualizerCount).toBe(6);

    const search = screen.getByRole("textbox", { name: "Search skills" });
    await userEvent.type(search, "design");
    expect(await screen.findByText("design-skill")).toBeTruthy();
    expect(screen.getByTestId("visible-count").textContent).toBe("1");
    expect(mocks.virtualizerCount).toBe(1);
  });

  it("uses flat view immediately when persisted filters are active", async () => {
    mocks.viewState.filters.origins = ["manual"];
    renderPage();
    expect(await screen.findByText("Results from all folders")).toBeTruthy();
    expect(screen.getByText("design-skill")).toBeTruthy();
    expect(screen.getByText("Engineering / Frontend")).toBeTruthy();
  });

  it("keeps every skill visible with a retry when the tree cannot load", async () => {
    mocks.folderTree = null;
    mocks.folderTreeError = true;
    renderPage();
    expect(await screen.findByText("placed-skill")).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("design-skill")).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(screen.getByText("Folders are unavailable. Showing skills in a flat list.")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetchTree).toHaveBeenCalled();
  });
});

describe("SkillsPage review regressions", () => {
  it("localizes folder expansion controls", async () => {
    renderPage("/acme/skills", "ja");
    expect(await screen.findByRole("button", { name: "「Engineering」を展開" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "すべて展開" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "すべて折りたたむ" })).toBeTruthy();
  });

  it("shows a skeleton instead of flashing the flat list while the tree loads", () => {
    mocks.folderTree = undefined;
    renderPage();
    // Positive assertions (N2): the loading skeleton is what renders, never
    // a premature empty-folder or flat view.
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(screen.queryByText("This folder is empty.")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand Engineering" })).toBeNull();
    expect(screen.queryByText("Results from all folders")).toBeNull();
  });

  it("keeps the upstream empty-workspace state", async () => {
    mocks.skills = [];
    mocks.folderTree = { folders: [], placements: [], packages: [] };
    renderPage();
    expect(await screen.findByText("No skills yet")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "New skill" }).length).toBeGreaterThan(0);
    expect(screen.queryByText("No skills or folders yet.")).toBeNull();
  });

  it("collapses a nested folder without collapsing its parent", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Expand Frontend" }));
    await userEvent.click(screen.getByRole("button", { name: "Collapse Frontend" }));
    expect(screen.queryByText("placed-skill")).toBeNull();
    expect(screen.queryByText("gray-skill")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Frontend" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
  });

  it("disables all package actions for a non-importer without admin rights", async () => {
    mocks.currentUserId = "someone-else";
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Engineering" }));
    expect(
      await screen.findByText("Only the package importer or an admin can do this."),
    ).toBeTruthy();
    for (const name of ["Rescan", "Rename", "Move", "Dissolve package", "Delete package"]) {
      expect(screen.getByText(name).closest("[data-disabled]")).toBeTruthy();
    }
  });

  it("enables package actions for an admin who did not import the package", async () => {
    mocks.currentUserId = "someone-else";
    mocks.members = [{ user_id: "someone-else", role: "admin" }];
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Engineering" }));
    expect(await screen.findByText("Rescan")).toBeTruthy();
    expect(screen.queryByText("Only the package importer or an admin can do this.")).toBeNull();
    for (const name of ["Rescan", "Rename", "Move", "Dissolve package", "Delete package"]) {
      expect(screen.getByText(name).closest("[data-disabled]")).toBeNull();
    }
  });

  it("opens dissolve and delete dialogs from their distinct package actions", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Engineering" }));
    await userEvent.click(await screen.findByText("Dissolve package"));
    expect(await screen.findByTestId("remove-dialog-dissolve")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Actions for Engineering" }));
    await userEvent.click(await screen.findByText("Delete package"));
    expect(await screen.findByTestId("remove-dialog-delete")).toBeTruthy();
  });

  it("limits selection and select-all to skill rows visible in the expanded tree", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand Design" }));
    const designRow = (await screen.findByText("design-skill")).closest("[role='row']")!;
    await userEvent.click(designRow.querySelector("button")!);
    expect(screen.getByTestId("batch-selection").textContent).toBe("design");

    await userEvent.click(screen.getByRole("button", { name: "Collapse Design" }));
    expect(screen.getByTestId("batch-selection").textContent).toBe("");
    await userEvent.click(screen.getByRole("button", { name: "Expand Design" }));
    expect(screen.getByTestId("batch-selection").textContent).toBe("design");

    await userEvent.click(screen.getByRole("button", { name: "Collapse Design" }));
    const header = screen.getAllByRole("row")[0]!;
    await userEvent.click(within(header).getAllByRole("button")[0]!);
    expect(screen.getByTestId("batch-selection").textContent).toBe("loose");
  });

  it("keeps folder rows first and uses composite keys for virtualizer and React rows", async () => {
    renderPage();
    const packageButton = await screen.findByRole("button", { name: "Expand Engineering" });
    expect(packageButton.textContent).toContain("Package");
    expect(packageButton.textContent).toContain("2 skills");
    const looseRow = screen.getByText("loose-skill").closest("[role='row']")!;

    await userEvent.click(packageButton);
    expect(mocks.virtualizerCount).toBe(6);
    expect(mocks.virtualizerKeys).toEqual([
      "folder-f-design",
      "folder-f-empty",
      "folder-f-eng",
      "folder-f-eng-fe",
      "entry-f-eng-skill-engineering",
      "entry-root-skill-loose",
    ]);
    const folderRow = (await screen.findByRole("button", { name: "Expand Frontend" })).closest("[role='row']")!;
    const skillRow = screen.getByText("engineering-skill").closest("[role='row']")!;
    expect(folderRow.compareDocumentPosition(skillRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByText("loose-skill").closest("[role='row']")).toBe(looseRow);
  });
});

describe("SkillsPage unreadable write responses", () => {
  it("refreshes the tree and shows an indeterminate result when a folder create reads back null", async () => {
    const { invalidateSkillPackageQueries } = await import(
      "@multica/core/skills/package-queries"
    );
    apiMock.createSkillFolder.mockResolvedValue(null);
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateSkillPackageQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't save the folder. Try again.")).toBeNull();
  });

  it("refreshes the tree and shows an indeterminate result when a folder move reads back null", async () => {
    const { invalidateSkillPackageQueries } = await import(
      "@multica/core/skills/package-queries"
    );
    apiMock.updateSkillFolder.mockResolvedValue(null);
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Move"));
    await userEvent.click(await screen.findByRole("button", { name: "Empty" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.updateSkillFolder).toHaveBeenCalledWith("ws-1", "f-design", { parent_id: "f-empty" }),
    );
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateSkillPackageQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't move the folder. Try again.")).toBeNull();
  });

  it("refreshes the tree and shows an indeterminate result when a folder delete reads back null", async () => {
    const { invalidateSkillPackageQueries } = await import(
      "@multica/core/skills/package-queries"
    );
    apiMock.deleteSkillFolder.mockResolvedValue(null);
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(apiMock.deleteSkillFolder).toHaveBeenCalledWith("ws-1", "f-design"),
    );
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateSkillPackageQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't delete the folder. Try again.")).toBeNull();
  });
});

describe("SkillsPage localized folder errors", () => {
  it("localizes a coded create error instead of showing the server message", async () => {
    const { ApiError } = await import("@multica/core/api");
    const { invalidateSkillPackageQueries } = await import(
      "@multica/core/skills/package-queries"
    );
    apiMock.createSkillFolder.mockRejectedValue(
      new ApiError("a folder with this name already exists at the destination", 409, "Conflict", {
        code: "name_conflict",
        error: "a folder with this name already exists at the destination",
      }),
    );
    renderPage("/acme/skills", "zh-Hans");
    await userEvent.click(await screen.findByRole("button", { name: "新建文件夹" }));
    await userEvent.type(screen.getByLabelText("名称"), "Research");
    await userEvent.click(screen.getByRole("button", { name: "创建" }));
    expect(await screen.findByText("目标位置已存在同名文件夹，请换一个名称。")).toBeTruthy();
    expect(screen.queryByText(/already exists at the destination/)).toBeNull();
    // A failed write still refreshes the tree — the copy says so.
    expect(invalidateSkillPackageQueries).toHaveBeenCalled();
  });

  // Every code the dialog's mapping claims to localize must render its own
  // copy — deleting a branch must fail a test.
  it.each([
    { code: "name_conflict", expected: "A folder with this name already exists at the destination." },
    { code: "folder_cycle", expected: "A folder can't be moved into one of its own subfolders." },
    {
      code: "invalid_name",
      expected:
        "The folder name isn't valid: 1–255 bytes, no surrounding whitespace, no control characters or line breaks.",
    },
    { code: "forbidden", expected: "You don't have permission to do this." },
    {
      code: "not_found",
      expected: "The target couldn't be found — it may have been deleted. The list was refreshed.",
    },
    {
      code: "managed_folder",
      expected: "Managed package folders follow the package and can't be changed here.",
    },
    { code: "preview_stale", expected: "The folder tree changed. The list was refreshed — try again." },
    {
      code: "permission_changed",
      expected: "Your permissions changed. The list was refreshed — try again.",
    },
  ])("localizes the $code error", async ({ code, expected }) => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.createSkillFolder.mockRejectedValue(
      new ApiError(`server says ${code}`, 409, "Conflict", { code, error: `server says ${code}` }),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText(expected)).toBeTruthy();
    expect(screen.queryByText(`server says ${code}`)).toBeNull();
  });

  it("rejects an over-long folder name before calling the server", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    // 86 CJK characters = 258 UTF-8 bytes, over the 255-byte contract limit.
    await userEvent.type(screen.getByLabelText("Name"), "文".repeat(86));
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(
      await screen.findByText(
        "The folder name isn't valid: 1–255 bytes, no surrounding whitespace, no control characters or line breaks.",
      ),
    ).toBeTruthy();
    expect(apiMock.createSkillFolder).not.toHaveBeenCalled();
  });

  it("localizes a coded move error", async () => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.updateSkillFolder.mockRejectedValue(
      new ApiError("folder move would create a cycle", 409, "Conflict", {
        code: "folder_cycle",
        error: "folder move would create a cycle",
      }),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Move"));
    await userEvent.click(await screen.findByRole("button", { name: "Empty" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(
      await screen.findByText("A folder can't be moved into one of its own subfolders."),
    ).toBeTruthy();
    expect(screen.queryByText("folder move would create a cycle")).toBeNull();
  });

  it("localizes a coded delete error", async () => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.deleteSkillFolder.mockRejectedValue(
      new ApiError("object not found in this workspace", 404, "Not Found", {
        code: "not_found",
        error: "object not found in this workspace",
      }),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    expect(
      await screen.findByText(
        "The target couldn't be found — it may have been deleted. The list was refreshed.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("object not found in this workspace")).toBeNull();
  });

  it("falls back to the generic failure copy for an uncoded error", async () => {
    const { invalidateSkillPackageQueries } = await import(
      "@multica/core/skills/package-queries"
    );
    apiMock.createSkillFolder.mockRejectedValue(new Error("socket hangup"));
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("Couldn't save the folder. Try again.")).toBeTruthy();
    expect(screen.queryByText("socket hangup")).toBeNull();
    expect(invalidateSkillPackageQueries).toHaveBeenCalled();
  });
});

describe("SkillsPage tree wait fallback", () => {
  it("keeps the expanded hierarchy when a background tree refresh fails once with cached data", () => {
    // Cached tree + first failed background attempt (retry still pending):
    // the hierarchy must not flip to flat for a transient failure.
    mocks.folderTreeFailures = 1;
    window.localStorage.setItem(
      "multica.folder_explorer.expanded.skills.ws-1",
      '["f-eng"]',
    );
    renderPage();
    expect(screen.getByRole("button", { name: "Expand Frontend" })).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(screen.queryByText("design-skill")).toBeNull();
    expect(
      screen.queryByText("Folders are unavailable. Showing skills in a flat list."),
    ).toBeNull();
  });

  it("falls back to the flat list on the first failed tree attempt, before the retry settles", () => {
    mocks.folderTree = undefined;
    mocks.folderTreeFailures = 1;
    renderPage();
    expect(screen.getByText("placed-skill")).toBeTruthy();
    expect(screen.getByText("engineering-skill")).toBeTruthy();
    expect(screen.getByText("design-skill")).toBeTruthy();
    expect(screen.getByText("loose-skill")).toBeTruthy();
    expect(
      screen.getByText("Folders are unavailable. Showing skills in a flat list."),
    ).toBeTruthy();
    expect(screen.queryByTestId("skeleton")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand Engineering" })).toBeNull();
  });

  it("falls back to the flat list after 5s without tree data, then restores the hierarchy when it arrives", async () => {
    vi.useFakeTimers();
    try {
      mocks.folderTree = undefined;
      window.localStorage.setItem(
        "multica.folder_explorer.expanded.skills.ws-1",
        '["f-eng","f-eng-fe"]',
      );
      const { controller, rerender } = renderPage();
      // Inside the grace window the skeleton shows; nothing flat flashes.
      expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
      expect(screen.queryByText("placed-skill")).toBeNull();

      // One millisecond before the deadline the grace window must still hold
      // (guards the 5s threshold itself).
      act(() => {
        vi.advanceTimersByTime(4999);
      });
      expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
      expect(screen.queryByText("placed-skill")).toBeNull();
      expect(
        screen.queryByText("Folders are unavailable. Showing skills in a flat list."),
      ).toBeNull();

      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByTestId("skeleton")).toBeNull();
      expect(screen.getByText("placed-skill")).toBeTruthy();
      expect(screen.getByText("loose-skill")).toBeTruthy();
      expect(
        screen.getByText("Folders are unavailable. Showing skills in a flat list."),
      ).toBeTruthy();

      // Late data restores the persisted expansion without changing the route.
      mocks.folderTree = folderTree;
      rerender(
        <NavigationHarness controller={controller} initialPath="/acme/skills">
          <SkillsPage />
        </NavigationHarness>,
      );
      expect(screen.queryByText("Folders are unavailable. Showing skills in a flat list.")).toBeNull();
      expect(screen.getByRole("button", { name: "Collapse Engineering" })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Collapse Frontend" })).toBeTruthy();
      expect(screen.getByText("placed-skill")).toBeTruthy();
      expect(screen.getByText("engineering-skill")).toBeTruthy();
      expect(screen.getByText("loose-skill")).toBeTruthy();
      expect(screen.queryByText("design-skill")).toBeNull();
      expect(controller.path).toBe("/acme/skills");
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("SkillsPage folder rows without skills", () => {
  it("keeps folder rows when folders exist but the workspace has 0 skills (N3)", async () => {
    mocks.skills = [];
    renderPage();
    expect(await screen.findByRole("button", { name: "Expand Engineering" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand Empty" })).toBeTruthy();
    // The upstream empty-workspace state stays hidden while folders exist.
    expect(screen.queryByText("No skills yet")).toBeNull();
    expect(screen.getByTestId("visible-count").textContent).toBe("0");
    expect(mocks.virtualizerCount).toBe(3);
  });
});

describe("SkillsPage select-all scope", () => {
  it("selects only visible imported skills once, never candidates or collapsed rows (M05)", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand all" }));
    const header = screen.getAllByRole("row")[0]!;
    await userEvent.click(within(header).getAllByRole("button")[0]!);
    const selected = screen.getByTestId("batch-selection").textContent!.split(",");
    expect(selected).toEqual(["design", "placed", "engineering", "loose"]);
    expect(new Set(selected).size).toBe(selected.length);

    // Collapsed skills leave the projection; the still-visible root skill stays.
    await userEvent.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.getByTestId("batch-selection").textContent).toBe("loose");
  });
});

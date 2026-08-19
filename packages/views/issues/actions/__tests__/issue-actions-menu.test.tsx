import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask, Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enIssues from "../../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// ---------------------------------------------------------------------------
// Mocks — same pattern as the issue-detail test suite.
// ---------------------------------------------------------------------------

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockOpenModal = vi.fn();
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: any) => {
      const state = { open: mockOpenModal };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

const mockAuthState = { user: { id: "user-1" }, isAuthenticated: true };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => (selector ? selector(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  ),
  registerAuthStore: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () =>
      Promise.resolve([
        { user_id: "user-1", name: "Test User", email: "t@t.com", role: "admin" },
      ]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
  squadListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "squads"],
    queryFn: () => Promise.resolve([]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "assignee-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_t: string, _id: string) => "" }),
}));

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({
    queryKey: ["pins", "ws-1", "user-1"],
    queryFn: () => Promise.resolve([]),
  }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

// Module-level flag toggled per-test: desktop implements `openInNewTab`,
// web omits it and the menu has to fall back to a real browser tab.
const { openInNewTabMock, getShareableUrlMock, navState } = vi.hoisted(() => ({
  openInNewTabMock: vi.fn(),
  getShareableUrlMock: vi.fn((p: string) => `https://app.example${p}`),
  navState: { hasOpenInNewTab: true },
}));

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/issues/issue-1",
    searchParams: new URLSearchParams(),
    back: vi.fn(),
    replace: vi.fn(),
    ...(navState.hasOpenInNewTab ? { openInNewTab: openInNewTabMock } : {}),
    getShareableUrl: getShareableUrlMock,
  }),
}));

const { toastSuccessMock } = vi.hoisted(() => ({
  toastSuccessMock: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: toastSuccessMock, error: vi.fn() },
}));

const { apiMocks, copyTextMock } = vi.hoisted(() => ({
  apiMocks: {
    listTasksByIssue: vi.fn(),
  },
  copyTextMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: apiMocks,
}));

vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: copyTextMock,
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: any) => <span data-testid="actor">{actorId}</span>,
}));

// Import after mocks.
import { IssueActionsDropdown } from "../issue-actions-dropdown";
import {
  IssueActionsContextMenu,
  IssueContextMenuProvider,
} from "../issue-actions-context-menu";

const listTasksByIssueMock = apiMocks.listTasksByIssue;

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  start_date: null,
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Issue;

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  mockOpenModal.mockReset();
  openInNewTabMock.mockReset();
  getShareableUrlMock.mockClear();
  navState.hasOpenInNewTab = true;
  copyTextMock.mockReset();
  copyTextMock.mockResolvedValue(true);
  toastSuccessMock.mockReset();
  listTasksByIssueMock.mockReset();
  listTasksByIssueMock.mockResolvedValue([]);
});

describe("IssueActionsDropdown", () => {
  it("renders the top-level items when the trigger is clicked", async () => {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));

    // Base UI portals the popup; role=menu lands on the popup wrapper.
    expect(await screen.findByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Priority")).toBeInTheDocument();
    expect(screen.getByText("Assignee")).toBeInTheDocument();
    expect(screen.getByText("Due date")).toBeInTheDocument();
    expect(screen.getByText("Open in new tab")).toBeInTheDocument();
    expect(screen.getByText("Copy link")).toBeInTheDocument();
    expect(screen.getByText("Relations")).toBeInTheDocument();
    expect(screen.getByText("Delete issue")).toBeInTheDocument();
    // Relationship actions are hidden inside the "Relations" submenu by default.
    expect(screen.queryByText("Create sub-issue")).not.toBeInTheDocument();
    expect(screen.queryByText("Set parent issue...")).not.toBeInTheDocument();
    expect(screen.queryByText("Add sub-issue...")).not.toBeInTheDocument();
  });

  it("clicking the Assignee item opens the shared AssigneePicker popover", async () => {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Assignee"));

    // The shared picker exposes a search input and renders the workspace
    // member under a "Members" group — both come from `AssigneePicker`, not
    // the legacy submenu (which had neither).
    expect(
      await screen.findByPlaceholderText("Assign to..."),
    ).toBeInTheDocument();
    expect(await screen.findByText("Members")).toBeInTheDocument();
    expect(await screen.findByText("Test User")).toBeInTheDocument();
  });

  it("shows 'Remove parent issue' in the Relations submenu only when the issue has a parent", async () => {
    const childIssue = { ...mockIssue, parent_issue_id: "parent-1" } as Issue;
    render(
      wrap(
        <IssueActionsDropdown
          issue={childIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Relations"));

    expect(await screen.findByText("Remove parent issue")).toBeInTheDocument();
  });

  it("hides 'Remove parent issue' when the issue has no parent", async () => {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Relations"));

    // The sibling "Set parent issue..." proves the submenu opened.
    expect(await screen.findByText("Set parent issue...")).toBeInTheDocument();
    expect(screen.queryByText("Remove parent issue")).not.toBeInTheDocument();
  });

  it("clicking Delete issue opens the delete-confirm modal", async () => {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
          onDeletedFallbackPath="/test/issues"
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    const del = await screen.findByText("Delete issue");
    fireEvent.click(del);

    expect(mockOpenModal).toHaveBeenCalledWith("issue-delete-confirm", {
      issueId: "issue-1",
      identifier: "TES-1",
      onDeletedFallbackPath: "/test/issues",
    });
  });

  it("copies the durable local project path for a worktree task", async () => {
    listTasksByIssueMock.mockResolvedValue([
      {
        status: "completed",
        created_at: "2026-08-18T10:00:00Z",
        work_dir: "/managed/task/worktree",
        durable_work_dir: "/Users/dev/project",
        branch_name: "agent/j/abc12345",
      } as AgentTask,
    ]);

    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Copy project directory path"));

    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith("/Users/dev/project");
      expect(toastSuccessMock).toHaveBeenCalledWith(
        "Project directory path copied — work is on agent/j/abc12345",
      );
    });
  });

  it("keeps a live task on its actual workdir", async () => {
    listTasksByIssueMock.mockResolvedValue([
      {
        status: "running",
        created_at: "2026-08-18T10:00:00Z",
        work_dir: "/managed/task/worktree",
        durable_work_dir: "/Users/dev/project",
      } as AgentTask,
    ]);

    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    await waitFor(() => {
      expect(listTasksByIssueMock).toHaveBeenCalledWith("issue-1");
    });
    fireEvent.click(await screen.findByText("Copy local workdir path"));

    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith("/managed/task/worktree");
    });
  });
});

describe("Open in new tab", () => {
  async function openMenuAndClickOpenInNewTab() {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );
    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Open in new tab"));
  }

  it("uses the desktop adapter and focuses the new tab", async () => {
    const windowOpen = vi
      .spyOn(window, "open")
      .mockReturnValue(null as unknown as Window);

    await openMenuAndClickOpenInNewTab();

    // `activate: true` — an explicit CTA moves the user into the new context,
    // unlike modifier-click, which stashes a background tab. The path carries
    // the identifier, matching copyLink, so the opened tab is already on the
    // canonical URL instead of being rewritten off the UUID after it lands.
    expect(openInNewTabMock).toHaveBeenCalledWith(
      "/test/issues/TES-1",
      "TES-1",
      { activate: true },
    );
    expect(windowOpen).not.toHaveBeenCalled();

    windowOpen.mockRestore();
  });

  it("falls back to a browser tab when the adapter is absent (web)", async () => {
    navState.hasOpenInNewTab = false;
    const windowOpen = vi
      .spyOn(window, "open")
      .mockReturnValue(null as unknown as Window);

    await openMenuAndClickOpenInNewTab();

    expect(openInNewTabMock).not.toHaveBeenCalled();
    expect(getShareableUrlMock).toHaveBeenCalledWith("/test/issues/TES-1");
    expect(windowOpen).toHaveBeenCalledWith(
      "https://app.example/test/issues/TES-1",
      "_blank",
      "noopener,noreferrer",
    );

    windowOpen.mockRestore();
  });
});

describe("IssueActionsContextMenu", () => {
  it("renders the menu when the wrapped element receives a contextmenu event", async () => {
    render(
      wrap(
        <IssueContextMenuProvider>
          <IssueActionsContextMenu issue={mockIssue}>
            <div data-testid="row">Row</div>
          </IssueActionsContextMenu>
        </IssueContextMenuProvider>,
      ),
    );

    fireEvent.contextMenu(screen.getByTestId("row"));

    expect(await screen.findByText("Status")).toBeInTheDocument();
    // The right-click surface is what list rows, board cards, gantt bars and
    // sub-issue rows all share, so this one assertion covers them together.
    expect(screen.getByText("Open in new tab")).toBeInTheDocument();
    expect(screen.getByText("Delete issue")).toBeInTheDocument();
  });
});

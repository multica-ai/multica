import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";

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

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/issues/issue-1",
    searchParams: new URLSearchParams(),
    back: vi.fn(),
    replace: vi.fn(),
  }),
}));

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: any) => <span data-testid="actor">{actorId}</span>,
}));

// Import after mocks.
import { IssueActionsDropdown } from "../issue-actions-dropdown";
import { IssueActionsContextMenu } from "../issue-actions-context-menu";

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
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Issue;

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  mockOpenModal.mockReset();
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
    expect(screen.getByText("Copy link")).toBeInTheDocument();
    expect(screen.getByText("More")).toBeInTheDocument();
    expect(screen.getByText("Delete issue")).toBeInTheDocument();
    // Relationship actions are hidden inside the "More" submenu by default.
    expect(screen.queryByText("Create sub-issue")).not.toBeInTheDocument();
    expect(screen.queryByText("Set parent issue...")).not.toBeInTheDocument();
    expect(screen.queryByText("Add sub-issue...")).not.toBeInTheDocument();
  });

  it("clicking Delete issue opens the delete-confirm modal", async () => {
    render(
      wrap(
        <IssueActionsDropdown
          issue={mockIssue}
          trigger={<button data-testid="trigger">Menu</button>}
          onDeletedNavigateTo="/test/issues"
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    const del = await screen.findByText("Delete issue");
    fireEvent.click(del);

    expect(mockOpenModal).toHaveBeenCalledWith("issue-delete-confirm", {
      issueId: "issue-1",
      identifier: "TES-1",
      onDeletedNavigateTo: "/test/issues",
    });
  });
});

describe("IssueActionsContextMenu", () => {
  it("renders the menu when the wrapped element receives a contextmenu event", async () => {
    render(
      wrap(
        <IssueActionsContextMenu issue={mockIssue}>
          <div data-testid="row">Row</div>
        </IssueActionsContextMenu>,
      ),
    );

    fireEvent.contextMenu(screen.getByTestId("row"));

    expect(await screen.findByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Delete issue")).toBeInTheDocument();
  });
});

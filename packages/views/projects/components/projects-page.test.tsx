import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => {
  const state = {
    user: { id: "user-1", email: "t@t.com", name: "T" },
    isAuthenticated: true,
  };
  return {
    useAuthStore: Object.assign(
      (selector?: any) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
    registerAuthStore: vi.fn(),
    createAuthStore: vi.fn(),
  };
});

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

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...rest }: any) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/projects" }),
  NavigationProvider: ({ children }: any) => children,
}));

const mockProjects: Project[] = [
  {
    id: "p-1",
    workspace_id: "ws-1",
    title: "Original Title",
    description: null,
    icon: "🚀",
    status: "in_progress",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    target_date: null,
    issue_count: 0,
    done_count: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as unknown as Project,
];

const mockMutate = vi.fn();

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({
    queryKey: ["projects", "ws-1"],
    queryFn: () => Promise.resolve(mockProjects),
  }),
  // ProjectsPage reads archivedProjectListOptions when "Show archived"
  // is toggled. The mock factory replaces the entire module, so any
  // export consumed by the component must be declared here — otherwise
  // vitest throws "No <name> export is defined on the … mock".
  archivedProjectListOptions: () => ({
    queryKey: ["projects", "ws-1", "archived"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useUpdateProject: () => ({ mutate: mockMutate }),
}));

vi.mock("@multica/core/projects/config", () => ({
  PROJECT_STATUS_CONFIG: {
    in_progress: { label: "In Progress", badgeBg: "", badgeText: "", dotColor: "" },
    backlog: { label: "Backlog", badgeBg: "", badgeText: "", dotColor: "" },
    completed: { label: "Completed", badgeBg: "", badgeText: "", dotColor: "" },
    cancelled: { label: "Cancelled", badgeBg: "", badgeText: "", dotColor: "" },
    paused: { label: "Paused", badgeBg: "", badgeText: "", dotColor: "" },
    planned: { label: "Planned", badgeBg: "", badgeText: "", dotColor: "" },
  },
  PROJECT_STATUS_ORDER: ["backlog", "planned", "in_progress", "paused", "completed", "cancelled"],
  PROJECT_PRIORITY_CONFIG: {
    none: { label: "No priority", color: "" },
    low: { label: "Low", color: "" },
    medium: { label: "Medium", color: "" },
    high: { label: "High", color: "" },
    urgent: { label: "Urgent", color: "" },
  },
  PROJECT_PRIORITY_ORDER: ["none", "low", "medium", "high", "urgent"],
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members", "ws-1"],
    queryFn: () => Promise.resolve([]),
  }),
  agentListOptions: () => ({
    queryKey: ["agents", "ws-1"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "" }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

vi.mock("../../issues/components/priority-icon", () => ({
  PriorityIcon: () => <span data-testid="priority-icon" />,
}));

vi.mock("./project-icon", () => ({
  ProjectIcon: () => <span data-testid="project-icon" />,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children, ...rest }: any) => <header {...rest}>{children}</header>,
}));

// dnd-kit is heavy to instantiate in jsdom and the real PointerSensor needs
// pointer events that are awkward to forge. Capture the DndContext handlers
// so tests can fire synthetic drag-end events directly — this proves the
// handler logic without depending on the sensor pipeline. Same pattern used
// by issues/components/list-view.test.tsx.
type CapturedHandlers = {
  onDragEnd: ((e: any) => void) | null;
};
const dndHandlers: CapturedHandlers = vi.hoisted(() => ({ onDragEnd: null }));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragEnd }: any) => {
    dndHandlers.onDragEnd = onDragEnd ?? null;
    return <div data-testid="dnd-context">{children}</div>;
  },
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
  pointerWithin: vi.fn(),
  closestCenter: vi.fn(),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: any) => children,
  verticalListSortingStrategy: {},
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

vi.mock("@dnd-kit/utilities", () => ({
  CSS: { Transform: { toString: () => undefined } },
}));

// ---------------------------------------------------------------------------
// Imports under test
// ---------------------------------------------------------------------------

import { ProjectsPage } from "./projects-page";

function renderWithQuery(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  // Reset the project list to a known initial state — tests that mutate
  // titles are testing the rename UI, not the round-trip back into the
  // query cache (that's covered by the mutations layer).
  mockProjects[0]!.title = "Original Title";
  dndHandlers.onDragEnd = null;
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ProjectsPage — inline title rename", () => {
  it("renders the project title as a link by default (no input)", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");
    expect(screen.queryByLabelText("Project name")).not.toBeInTheDocument();
  });

  it("flips into edit mode on double-click and shows an input prefilled with the current title", async () => {
    renderWithQuery(<ProjectsPage />);
    const title = await screen.findByText("Original Title");
    fireEvent.doubleClick(title);

    const input = await screen.findByLabelText<HTMLInputElement>("Project name");
    expect(input).toBeInTheDocument();
    expect(input.value).toBe("Original Title");
  });

  it("renders a hover-revealed Rename button next to the title", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");
    const renameBtn = screen.getByRole("button", { name: /rename original title/i });
    expect(renameBtn).toBeInTheDocument();
  });

  it("flips into edit mode when the pencil button is clicked (no double-click required)", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");
    fireEvent.click(screen.getByRole("button", { name: /rename original title/i }));

    const input = await screen.findByLabelText<HTMLInputElement>("Project name");
    expect(input).toBeInTheDocument();
    expect(input.value).toBe("Original Title");
  });

  it("commits a renamed title on Enter and calls updateProject", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText<HTMLInputElement>("Project name");

    fireEvent.change(input, { target: { value: "New Title" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(mockMutate).toHaveBeenCalledTimes(1);
    expect(mockMutate).toHaveBeenCalledWith({ id: "p-1", title: "New Title" });
    // Input is gone — back to display mode.
    await waitFor(() =>
      expect(screen.queryByLabelText("Project name")).not.toBeInTheDocument(),
    );
  });

  it("commits a renamed title on blur", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText<HTMLInputElement>("Project name");

    fireEvent.change(input, { target: { value: "Renamed By Blur" } });
    fireEvent.blur(input);

    expect(mockMutate).toHaveBeenCalledWith({
      id: "p-1",
      title: "Renamed By Blur",
    });
  });

  it("trims whitespace before committing", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText<HTMLInputElement>("Project name");

    fireEvent.change(input, { target: { value: "   Spaced Title   " } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(mockMutate).toHaveBeenCalledWith({ id: "p-1", title: "Spaced Title" });
  });

  it("does NOT call updateProject when the title is unchanged", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText("Project name");

    // No-op: same value.
    fireEvent.keyDown(input, { key: "Enter" });

    expect(mockMutate).not.toHaveBeenCalled();
  });

  it("does NOT call updateProject when the title is empty (silently reverts)", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText<HTMLInputElement>("Project name");

    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(mockMutate).not.toHaveBeenCalled();
    // And the original title is back on screen, not the empty draft.
    await screen.findByText("Original Title");
  });

  it("cancels on Escape and does NOT call updateProject", async () => {
    renderWithQuery(<ProjectsPage />);
    fireEvent.doubleClick(await screen.findByText("Original Title"));
    const input = await screen.findByLabelText<HTMLInputElement>("Project name");

    fireEvent.change(input, { target: { value: "Discarded Edit" } });
    fireEvent.keyDown(input, { key: "Escape" });

    expect(mockMutate).not.toHaveBeenCalled();
    // Display mode again, original title visible.
    await screen.findByText("Original Title");
    expect(screen.queryByLabelText("Project name")).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Drag-to-move tests
//
// Strategy mirrors issues/list-view: the @dnd-kit/core mock above captures
// the DndContext's onDragEnd handler so tests can fire synthetic drag
// events directly. This proves the handler logic without depending on
// the dnd-kit sensor pipeline.
// ---------------------------------------------------------------------------

describe("ProjectsPage — drag to move", () => {
  it("renders status section headers grouped by PROJECT_STATUS_ORDER", async () => {
    renderWithQuery(<ProjectsPage />);
    // The single mock project is in_progress; all status headers still
    // render (empty groups serve as drop targets), so every status label
    // appears at least once. "In Progress" appears twice — once in the
    // section header, once in the row's inline status dropdown — so we
    // use getAllByText and assert ≥1, which is sufficient to prove the
    // group header rendered.
    await screen.findByText("Original Title");
    expect(screen.getAllByText("In Progress").length).toBeGreaterThanOrEqual(1);
    // Empty status groups have a header but no row dropdown — exact-match.
    expect(screen.getByText("Planned")).toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
    expect(screen.getByText("Cancelled")).toBeInTheDocument();
    expect(screen.getByText("Paused")).toBeInTheDocument();
  });

  it("mounts a single DndContext for the active view", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");
    expect(screen.getByTestId("dnd-context")).toBeInTheDocument();
  });

  it("calls updateProject with new status when dropped on a different status section", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");

    // Synthetic drop: drag the "in_progress" project onto the
    // "completed" status section. The id encoding mirrors the production
    // STATUS_DROPPABLE_PREFIX from projects-page.tsx.
    dndHandlers.onDragEnd?.({
      active: { id: "p-1" },
      over: { id: "projects-status:completed" },
    });

    expect(mockMutate).toHaveBeenCalledTimes(1);
    expect(mockMutate).toHaveBeenCalledWith({ id: "p-1", status: "completed" });
  });

  it("calls updateProject with target status when dropped on a row in another status", async () => {
    // Add a second project in a different status so we can drop ONTO it.
    mockProjects.push({
      id: "p-2",
      workspace_id: "ws-1",
      title: "Other Project",
      description: null,
      icon: "📦",
      status: "completed",
      priority: "low",
      lead_type: null,
      lead_id: null,
      target_date: null,
      issue_count: 0,
      done_count: 0,
      created_at: "2026-01-02T00:00:00Z",
      updated_at: "2026-01-02T00:00:00Z",
    } as unknown as Project);

    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");

    // Drop p-1 (in_progress) onto p-2's row (completed) — the handler
    // should resolve the destination via p-2's status, not the row id.
    dndHandlers.onDragEnd?.({
      active: { id: "p-1" },
      over: { id: "p-2" },
    });

    expect(mockMutate).toHaveBeenCalledWith({ id: "p-1", status: "completed" });

    mockProjects.pop();
  });

  it("does NOT call updateProject when dropped within the same status (no position column)", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");

    // Drop in_progress project onto its own status section — same status,
    // so the cross-status branch doesn't fire and (with no position
    // column on the project table) reorder is a no-op.
    dndHandlers.onDragEnd?.({
      active: { id: "p-1" },
      over: { id: "projects-status:in_progress" },
    });

    expect(mockMutate).not.toHaveBeenCalled();
  });

  it("does NOT call updateProject when the drag is cancelled (over=null)", async () => {
    renderWithQuery(<ProjectsPage />);
    await screen.findByText("Original Title");

    dndHandlers.onDragEnd?.({ active: { id: "p-1" }, over: null });

    expect(mockMutate).not.toHaveBeenCalled();
  });
});

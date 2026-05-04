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

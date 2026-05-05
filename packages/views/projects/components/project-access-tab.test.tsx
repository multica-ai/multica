import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";

// ---------- Hoisted mocks ----------

const mockUpdateAccess = vi.hoisted(() => vi.fn());
const mockListProjectMembers = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ members: [] }),
);
const mockAddProjectMember = vi.hoisted(() => vi.fn());
const mockRemoveProjectMember = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    updateProjectAccess: mockUpdateAccess,
    listProjectMembers: mockListProjectMembers,
    addProjectMember: mockAddProjectMember,
    removeProjectMember: mockRemoveProjectMember,
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: any) => any) => {
      const state = {
        user: {
          id: "user-owner",
          email: "owner@multica.test",
          name: "Owner User",
        },
      };
      return selector ? selector(state) : state;
    },
    { getState: () => ({}) },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const memberList = [
  {
    id: "m-owner",
    user_id: "user-owner",
    name: "Owner User",
    email: "owner@multica.test",
    role: "owner",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
  {
    id: "m-admin",
    user_id: "user-admin",
    name: "Admin User",
    email: "admin@multica.test",
    role: "admin",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
  {
    id: "m-1",
    user_id: "user-1",
    name: "Anne Larsen",
    email: "anne@multica.test",
    role: "member",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
  {
    id: "m-2",
    user_id: "user-2",
    name: "Morten K",
    email: "morten@multica.test",
    role: "member",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
];

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["memberList"],
    queryFn: () => Promise.resolve(memberList),
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectKeys: {
    list: (wsId: string) => ["projects", wsId, "list"],
    detail: (wsId: string, id: string) => ["projects", wsId, "detail", id],
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={"avatar-" + actorId}>av</span>
  ),
}));

import { ProjectAccessTab } from "./project-access-tab";

// ---------- Helpers ----------

function makeProject(access: "workspace" | "restricted"): Project {
  return {
    id: "p-1",
    workspace_id: "ws-1",
    title: "HR 2026",
    description: null,
    icon: null,
    color: null,
    repo_url: null,
    status: "in_progress",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    access,
    created_at: "",
    updated_at: "",
    issue_count: 0,
    done_count: 0,
  };
}

function renderWith(project: Project) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <ProjectAccessTab project={project} />
    </QueryClientProvider>,
  );
}

// ---------- Tests ----------

describe("ProjectAccessTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListProjectMembers.mockResolvedValue({ members: [] });
  });

  it("renders both access options for an admin", async () => {
    renderWith(makeProject("workspace"));
    await waitFor(() => {
      expect(screen.getByText(/Open to workspace/i)).toBeInTheDocument();
      expect(screen.getByText(/Restricted/i)).toBeInTheDocument();
    });
  });

  it("shows the member picker when admin chooses restricted", async () => {
    renderWith(makeProject("workspace"));
    await screen.findByText(/Open to workspace/i);
    fireEvent.click(screen.getByText(/Restricted/i));
    await waitFor(() => {
      expect(screen.getByTestId("access-review-panel")).toBeInTheDocument();
    });
    // Pickable list = workspace members minus admins/self.
    expect(screen.getByText("Anne Larsen")).toBeInTheDocument();
    expect(screen.getByText("Morten K")).toBeInTheDocument();
    // The picker is interactive — checkboxes are present.
    const checkboxes = screen.getAllByRole("checkbox");
    expect(checkboxes.length).toBe(2);
    // Default: nobody picked → admins-only message in footer.
    expect(
      screen.getByText(/only admins will have access/i),
    ).toBeInTheDocument();
  });

  it("adds picked members before flipping access on confirm", async () => {
    mockAddProjectMember.mockResolvedValue({});
    mockUpdateAccess.mockResolvedValue({
      ...makeProject("restricted"),
    });
    const callOrder: string[] = [];
    mockAddProjectMember.mockImplementation((..._args) => {
      callOrder.push("addProjectMember");
      return Promise.resolve({});
    });
    mockUpdateAccess.mockImplementation((..._args) => {
      callOrder.push("updateProjectAccess");
      return Promise.resolve(makeProject("restricted"));
    });

    renderWith(makeProject("workspace"));
    await screen.findByText(/Open to workspace/i);
    fireEvent.click(screen.getByText(/Restricted/i));
    await screen.findByTestId("access-review-panel");

    // Pick Anne.
    const anneRow = screen.getByText("Anne Larsen").closest("button")!;
    fireEvent.click(anneRow);
    expect(
      screen.getByText(/1 person \+ admins will have access/i),
    ).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Make restricted/i }));
    });

    await waitFor(() => {
      expect(mockAddProjectMember).toHaveBeenCalledWith("p-1", "user-1");
      expect(mockUpdateAccess).toHaveBeenCalledWith("p-1", "restricted");
    });
    // Members must be added before the flip to avoid a brief lockout window.
    expect(callOrder).toEqual(["addProjectMember", "updateProjectAccess"]);
  });

  it("flips to restricted with admins-only when nobody is picked", async () => {
    mockUpdateAccess.mockResolvedValue({
      ...makeProject("restricted"),
    });
    renderWith(makeProject("workspace"));
    await screen.findByText(/Open to workspace/i);
    fireEvent.click(screen.getByText(/Restricted/i));
    await screen.findByTestId("access-review-panel");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Make restricted/i }));
    });

    await waitFor(() => {
      expect(mockUpdateAccess).toHaveBeenCalledWith("p-1", "restricted");
    });
    expect(mockAddProjectMember).not.toHaveBeenCalled();
  });

  it("renders read-only summary for non-admin members", async () => {
    // Re-mock auth as a regular member
    const memberOnly = vi.fn(() => ({
      user: {
        id: "user-1",
        email: "anne@multica.test",
        name: "Anne Larsen",
      },
    }));
    vi.doMock("@multica/core/auth", () => ({
      useAuthStore: Object.assign((selector?: (s: any) => any) => {
        const s = memberOnly();
        return selector ? selector(s) : s;
      }, { getState: () => ({}) }),
    }));
    vi.resetModules();
    const { ProjectAccessTab: Mod } = await import("./project-access-tab");
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={qc}>
        <Mod project={makeProject("restricted")} />
      </QueryClientProvider>,
    );
    await waitFor(() => {
      expect(
        screen.getByText(/This project is restricted/i),
      ).toBeInTheDocument();
      // Toggle radios should not be present
      expect(screen.queryByTestId("project-access-tab")).not.toBeInTheDocument();
    });
  });

  it("shows Project members panel and Always-on group when restricted", async () => {
    mockListProjectMembers.mockResolvedValue({
      members: [
        {
          user_id: "user-1",
          name: "Anne Larsen",
          email: "anne@multica.test",
          avatar_url: null,
          added_at: "2026-04-01T00:00:00Z",
        },
      ],
    });
    renderWith(makeProject("restricted"));
    await screen.findByText(/Project members/i);
    expect(screen.getByText(/Always-on access/i)).toBeInTheDocument();
    // Owner + Admin appear in the always-on rows
    const alwaysOn = screen.getAllByTestId("always-on-row");
    expect(alwaysOn.length).toBe(2);
  });
});

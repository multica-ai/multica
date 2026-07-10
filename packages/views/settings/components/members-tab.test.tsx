import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { MembersTab } from "./members-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

const mocks = vi.hoisted(() => ({
  invalidateQueries: vi.fn(),
  listMembers: vi.fn(),
  searchDeptUsers: vi.fn(),
  batchAddDeptMembers: vi.fn(),
  searchDeptDepartments: vi.fn(),
  listDeptDepartmentUsers: vi.fn(),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
    useQuery: (options: { queryKey?: readonly unknown[] }) => {
      const key = JSON.stringify(options.queryKey ?? []);
      if (key.includes("members")) return { data: mocks.listMembers() };
      return { data: [] };
    },
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "owner-user", name: "Owner", email: "owner@example.test" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({ memberDetail: (id: string) => `/acme/members/${id}` }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    searchDeptUsers: mocks.searchDeptUsers,
    searchDeptDepartments: mocks.searchDeptDepartments,
    listDeptDepartmentUsers: mocks.listDeptDepartmentUsers,
    batchAddDeptMembers: mocks.batchAddDeptMembers,
    updateMember: vi.fn(),
    deleteMember: vi.fn(),
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <div data-testid={`avatar-${actorId}`} />,
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("MembersTab", () => {
  beforeEach(() => {
    mocks.invalidateQueries.mockReset();
    mocks.searchDeptUsers.mockReset();
    mocks.searchDeptDepartments.mockReset();
    mocks.listDeptDepartmentUsers.mockReset();
    mocks.batchAddDeptMembers.mockReset();
    mocks.searchDeptUsers.mockResolvedValue([]);
    mocks.searchDeptDepartments.mockResolvedValue([]);
    mocks.listDeptDepartmentUsers.mockResolvedValue([]);
    mocks.listMembers.mockReturnValue([
      {
        id: "member-owner",
        workspace_id: "ws-1",
        user_id: "owner-user",
        role: "owner",
        source: "manual",
        status: "active",
        name: "Owner",
        email: "owner@example.test",
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "member-runtime",
        workspace_id: "ws-1",
        user_id: "runtime-user",
        role: "member",
        source: "dept",
        status: "active",
        external_user_id: "E004",
        external_universal_id: "uni-runtime",
        employee_id: "E004",
        name: "Runtime Dept User",
        email: "runtime@example.test",
        position: "SRE",
        dept_name: "Platform Runtime",
        dept_path: "Engineering/Platform Dept/Runtime",
        created_at: "2026-01-01T00:00:00Z",
        avatar_url: null,
      },
    ]);
  });

  it("searches dept users and departments from one search field and batch-adds selected members", async () => {
    mocks.searchDeptUsers.mockResolvedValue([
      {
        user_id: "E001",
        username: "Active Dept User",
        universal_id: "uni-active",
        dept_path: "/Root/Platform",
        dept_name: "Platform",
        position: "Engineer",
        status: 1,
      },
    ]);
    mocks.batchAddDeptMembers.mockResolvedValue({ added: 1, skipped: 0 });

    render(<MembersTab />, { wrapper: I18nWrapper });

    expect(screen.queryByText("Invite member")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("user@company.com")).not.toBeInTheDocument();
    expect(screen.getByText("Runtime Dept User(E004)")).toBeInTheDocument();
    expect(screen.getByText("Engineering/Platform Dept/Runtime SRE")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText(/employee name/i), {
      target: { value: "E001" },
    });

    expect(await screen.findByText("Active Dept User(E001)")).toBeInTheDocument();
    expect(screen.getByText("/Root/Platform Engineer")).toBeInTheDocument();
    expect(screen.getByTestId("dept-member-results")).toHaveClass("max-h-72", "overflow-y-auto");
    expect(mocks.searchDeptUsers).toHaveBeenCalledWith("E001");
    expect(mocks.searchDeptDepartments).toHaveBeenCalledWith("E001");
    fireEvent.click(screen.getByRole("checkbox", { name: /Active Dept User/i }));
    expect(screen.getByText("Selected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /add selected/i }));

    await waitFor(() =>
      expect(mocks.batchAddDeptMembers).toHaveBeenCalledWith("ws-1", {
        users: [{ external_user_id: "E001", external_universal_id: "uni-active" }],
      }),
    );
    expect(screen.getByText("Added 1 members. Skipped 0.")).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/employee name/i)).toHaveValue("");
    expect(screen.queryByText("Active Dept User(E001)")).not.toBeInTheDocument();
    expect(mocks.invalidateQueries).toHaveBeenCalled();
  });

  it("localizes workspace member status labels", () => {
    mocks.listMembers.mockReturnValue([
      {
        id: "member-owner",
        workspace_id: "ws-1",
        user_id: "owner-user",
        role: "owner",
        source: "manual",
        status: "active",
        name: "Owner",
        email: "owner@example.test",
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "member-pending",
        workspace_id: "ws-1",
        user_id: "pending-user",
        role: "member",
        source: "dept",
        status: "pending_activation",
        external_user_id: "E002",
        employee_id: "E002",
        name: "Pending Dept User",
        email: "pending@example.test",
        position: "Designer",
        dept_path: "Engineering/Design",
        created_at: "2026-01-01T00:00:00Z",
      },
    ]);

    render(<MembersTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("Pending activation")).toBeInTheDocument();
    expect(screen.queryByText("pending_activation")).not.toBeInTheDocument();
  });

  it("shows department fuzzy suggestions, expands members, and keeps selections across searches", async () => {
    mocks.searchDeptUsers
      .mockResolvedValueOnce([
        {
          user_id: "E001",
          username: "Active Dept User",
          universal_id: "uni-active",
          dept_name: "Platform",
          position: "Engineer",
          status: 1,
        },
      ])
      .mockResolvedValueOnce([]);
    mocks.searchDeptDepartments.mockResolvedValue([
      {
        dept_id: "D100",
        dept_name: "Platform Dept",
        dept_path: "Engineering/Platform Dept",
      },
    ]);
    mocks.listDeptDepartmentUsers.mockResolvedValue([
      {
        user_id: "E004",
        username: "Runtime Dept User",
        universal_id: "uni-runtime",
        dept_name: "Platform Runtime",
        dept_path: "Engineering/Platform Dept/Runtime",
        position: "SRE",
        status: 1,
      },
      {
        user_id: "E005",
        username: "Runtime Frontend User",
        universal_id: "uni-frontend",
        dept_name: "Platform Runtime",
        dept_path: "Engineering/Platform Dept/Runtime",
        position: "Frontend",
        status: 1,
      },
    ]);
    mocks.batchAddDeptMembers.mockResolvedValue({ added: 1, skipped: 0 });

    render(<MembersTab />, { wrapper: I18nWrapper });

    const searchBox = screen.getByPlaceholderText(/employee name/i);
    expect(screen.queryByPlaceholderText(/departments by name/i)).not.toBeInTheDocument();

    fireEvent.change(searchBox, {
      target: { value: "E001" },
    });
    fireEvent.click(await screen.findByRole("checkbox", { name: /Active Dept User/i }));
    expect(screen.getByText("1 selected")).toBeInTheDocument();

    fireEvent.change(searchBox, {
      target: { value: "platform" },
    });

    expect(await screen.findByText("View members")).toBeInTheDocument();
    expect(screen.getByText("Engineering/Platform Dept")).toBeInTheDocument();
    expect(screen.getByTestId("dept-department-results")).toHaveClass("max-h-72", "overflow-y-auto");
    fireEvent.change(searchBox, {
      target: { value: "" },
    });
    await waitFor(() => expect(screen.queryByText("Platform Dept")).not.toBeInTheDocument());

    fireEvent.change(searchBox, {
      target: { value: "platform" },
    });
    expect(await screen.findByText("View members")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: /Platform Dept/i }));
    expect(screen.queryByTestId("dept-department-results")).not.toBeInTheDocument();
    expect(await screen.findByText("Runtime Dept User(E004)")).toBeInTheDocument();
    expect(await screen.findByText("Runtime Frontend User(E005)")).toBeInTheDocument();
    expect(screen.getAllByText("Engineering/Platform Dept/Runtime SRE")).toHaveLength(2);
    expect(screen.getByText("Members in Platform Dept")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /Runtime Dept User/i })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Runtime Dept User/i })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByTestId("dept-selected-panel")).toBeInTheDocument();
    expect(screen.getByTestId("dept-selected-panel").compareDocumentPosition(screen.getByTestId("dept-member-results"))).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(screen.queryByRole("button", { name: /remove Runtime Dept User/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /back to departments/i }));
    expect(screen.getByText("Engineering/Platform Dept")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: /Platform Dept/i }));
    expect(await screen.findByText("Runtime Dept User(E004)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: /select all/i }));
    expect(screen.getByText("3 selected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /add selected/i }));

    await waitFor(() =>
      expect(mocks.batchAddDeptMembers).toHaveBeenCalledWith("ws-1", {
        users: [
          { external_user_id: "E001", external_universal_id: "uni-active" },
          { external_user_id: "E005", external_universal_id: "uni-frontend" },
        ],
      }),
    );
  });
});

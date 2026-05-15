// JEH-1067 (Bundle B / PR-B) — MemberGroupsSection + MemberGroupChips tests.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockListGroups = vi.hoisted(() => vi.fn());
const mockListGroupMembers = vi.hoisted(() => vi.fn());
const mockAddGroupMember = vi.hoisted(() => vi.fn());
const mockRemoveGroupMember = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
      listCerebroGroupMembers: mockListGroupMembers,
      addCerebroGroupMember: mockAddGroupMember,
      removeCerebroGroupMember: mockRemoveGroupMember,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import {
  MemberGroupsSection,
  MemberGroupChips,
} from "./member-groups-section";

function renderWith(node: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>{node}</QueryClientProvider>,
  );
}

describe("MemberGroupsSection", () => {
  beforeEach(() => vi.clearAllMocks());

  it("renders the groups the user is a member of and hides others from the picker", async () => {
    mockListGroups.mockResolvedValue([
      {
        id: "g-1",
        workspace_id: "ws-1",
        name: "Engineering",
        description: null,
        created_by: null,
        created_at: "",
        updated_at: "",
      },
      {
        id: "g-2",
        workspace_id: "ws-1",
        name: "Sales",
        description: null,
        created_by: null,
        created_at: "",
        updated_at: "",
      },
    ]);
    mockListGroupMembers.mockImplementation(async (groupId: string) => {
      if (groupId === "g-1") {
        return [
          {
            group_id: "g-1",
            user_id: "user-42",
            added_by: null,
            added_at: "",
            user_name: "Anne",
            user_email: "a@x",
            user_avatar_url: null,
          },
        ];
      }
      return [];
    });

    renderWith(<MemberGroupsSection userId="user-42" isAdmin={true} />);

    await waitFor(() =>
      expect(screen.getByTestId("member-groups-list")).toBeInTheDocument(),
    );
    expect(screen.getByText("Engineering")).toBeInTheDocument();
    expect(screen.queryByText("Sales")).not.toBeInTheDocument();
  });
});

describe("MemberGroupChips", () => {
  beforeEach(() => vi.clearAllMocks());

  it("renders one chip per group the user is in, collapsing overflow into +N", async () => {
    mockListGroups.mockResolvedValue([
      ...["g-a", "g-b", "g-c", "g-d", "g-e"].map((id) => ({
        id,
        workspace_id: "ws-1",
        name: id.toUpperCase(),
        description: null,
        created_by: null,
        created_at: "",
        updated_at: "",
      })),
    ]);
    mockListGroupMembers.mockImplementation(async () => [
      {
        group_id: "any",
        user_id: "user-42",
        added_by: null,
        added_at: "",
        user_name: "Anne",
        user_email: "a@x",
        user_avatar_url: null,
      },
    ]);

    renderWith(<MemberGroupChips userId="user-42" />);

    await waitFor(() =>
      expect(screen.getAllByTestId("member-group-chip").length).toBe(3),
    );
    expect(screen.getByText("+2")).toBeInTheDocument();
  });

  it("shows an em-dash when the user is in no groups", async () => {
    mockListGroups.mockResolvedValue([]);
    mockListGroupMembers.mockResolvedValue([]);

    renderWith(<MemberGroupChips userId="user-42" />);

    await waitFor(() => {
      expect(screen.getByText("—")).toBeInTheDocument();
    });
  });
});

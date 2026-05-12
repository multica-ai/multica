import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---------- Hoisted mocks ----------

const mockListGroups = vi.hoisted(() => vi.fn());
const mockListProjectGroups = vi.hoisted(() => vi.fn());
const mockAddProjectGroup = vi.hoisted(() => vi.fn());
const mockRemoveProjectGroup = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
      listCerebroProjectGroups: mockListProjectGroups,
      addCerebroProjectGroup: mockAddProjectGroup,
      removeCerebroProjectGroup: mockRemoveProjectGroup,
    },
  };
});

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

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["memberList"],
    queryFn: () =>
      Promise.resolve([
        {
          id: "m-owner",
          user_id: "user-owner",
          name: "Owner",
          email: "o@e.test",
          role: "owner",
          avatar_url: null,
          workspace_id: "ws-1",
          created_at: "",
        },
      ]),
  }),
}));

const mockUseFeatureFlag = vi.hoisted(() => vi.fn(() => true));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mockUseFeatureFlag,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { ProjectGroupAccessSection } from "./project-group-access-section";

function renderSection() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <ProjectGroupAccessSection projectId="p-1" />
    </QueryClientProvider>,
  );
}

describe("ProjectGroupAccessSection", () => {
  it("returns null when cerebro_groups_enabled is off", () => {
    mockUseFeatureFlag.mockReturnValueOnce(false);
    mockListGroups.mockResolvedValueOnce([]);
    mockListProjectGroups.mockResolvedValueOnce([]);
    const { container } = renderSection();
    expect(container.firstChild).toBeNull();
  });

  it("renders granted groups and exposes the add affordance for admins", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
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
    ]);
    mockListProjectGroups.mockResolvedValue([
      { project_id: "p-1", group_id: "g-1", added_by: null, added_at: "" },
    ]);

    renderSection();
    await waitFor(
      () => expect(screen.getByText("Engineering")).toBeTruthy(),
      { timeout: 15000 },
    );
    expect(screen.getByTestId("add-group-access-button")).toBeTruthy();
  }, 30000);

  it("renders the empty state when no groups have access", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
    mockListGroups.mockResolvedValueOnce([]);
    mockListProjectGroups.mockResolvedValueOnce([]);

    renderSection();
    await waitFor(() =>
      expect(screen.getByText(/No groups have been granted access/)).toBeTruthy(),
    );
  });
});

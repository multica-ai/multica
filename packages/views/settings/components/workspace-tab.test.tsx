import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockUseQuery = vi.hoisted(() => vi.fn());
const mockGetQueryData = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());
const mockLeaveMutate = vi.hoisted(() => vi.fn());
const mockDeleteMutate = vi.hoisted(() => vi.fn());
const mockNavigationPush = vi.hoisted(() => vi.fn());
const mockSetCurrentWorkspace = vi.hoisted(() => vi.fn());
const mockResolvePostAuthDestination = vi.hoisted(() => vi.fn(() => "/"));
const mockUseCurrentWorkspace = vi.hoisted(() => vi.fn());
const mockUseHasOnboarded = vi.hoisted(() => vi.fn(() => true));
const mockUseAuthStore = vi.hoisted(() => vi.fn());
const mockUseWorkspaceId = vi.hoisted(() => vi.fn(() => "ws-1"));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: unknown) => mockUseQuery(opts),
  useQueryClient: () => ({
    getQueryData: mockGetQueryData,
    setQueryData: mockSetQueryData,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (s: unknown) => unknown) => {
    const state = mockUseAuthStore();
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useLeaveWorkspace: () => ({ mutateAsync: mockLeaveMutate }),
  useDeleteWorkspace: () => ({ mutateAsync: mockDeleteMutate }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => mockUseWorkspaceId(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId] }),
  workspaceKeys: { list: () => ["workspaces", "list"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces", "list"] }),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: vi.fn() },
}));

vi.mock("@multica/core/paths", () => ({
  resolvePostAuthDestination: () => mockResolvePostAuthDestination(),
  useCurrentWorkspace: () => mockUseCurrentWorkspace(),
  useHasOnboarded: () => mockUseHasOnboarded(),
}));

vi.mock("@multica/core/platform", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/platform")>(
    "@multica/core/platform",
  );
  return {
    ...actual,
    setCurrentWorkspace: (...args: unknown[]) => mockSetCurrentWorkspace(...args),
  };
});

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: mockNavigationPush }),
}));

vi.mock("./delete-workspace-dialog", () => ({
  DeleteWorkspaceDialog: () => null,
}));

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  ProductCapabilitiesProvider,
  type ProductCapabilities,
} from "@multica/core/platform";
import { LOCAL_PRODUCT_CAPABILITIES } from "@multica/core/config";
import { WorkspaceTab } from "./workspace-tab";

const fullCapabilities: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  collaboration: {
    ...LOCAL_PRODUCT_CAPABILITIES.collaboration,
    allowLeaveWorkspace: true,
  },
};

const baseUser = { id: "user-1", email: "user@example.com", name: "User" };
const baseWorkspace = {
  id: "ws-1",
  slug: "acme",
  name: "Acme",
  description: "",
  context: "",
};

// Members list with an admin (current user) and an owner so isOwner=false,
// isSoleMember=false, and the Danger Zone renders.
const adminMembers = [
  { user_id: "user-1", role: "admin" },
  { user_id: "user-2", role: "owner" },
];

// Members list where the current user is owner, second is admin — owner is
// not the sole owner-of-record only because membership has another non-owner
// member; here we make members.length=2 and the user is owner so the leave
// button is enabled (not sole owner because we provide another owner).
const ownerMembers = [
  { user_id: "user-1", role: "owner" },
  { user_id: "user-2", role: "owner" },
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("WorkspaceTab capability gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAuthStore.mockReturnValue({ user: baseUser });
    mockUseCurrentWorkspace.mockReturnValue(baseWorkspace);
    mockUseWorkspaceId.mockReturnValue("ws-1");
  });

  it("hides Leave workspace under default (local) capabilities", () => {
    mockUseQuery.mockReturnValue({ data: adminMembers, isFetched: true });

    render(<WorkspaceTab />);

    expect(
      screen.queryByRole("button", { name: /leave workspace/i }),
    ).toBeNull();
    // Description text for the Leave row should also not render.
    expect(screen.queryByText(/remove yourself from this workspace/i)).toBeNull();
  });

  it("shows Leave workspace when capabilities allow", () => {
    mockUseQuery.mockReturnValue({ data: ownerMembers, isFetched: true });

    render(
      <ProductCapabilitiesProvider capabilities={fullCapabilities}>
        <WorkspaceTab />
      </ProductCapabilitiesProvider>,
    );

    expect(
      screen.getByRole("button", { name: /leave workspace/i }),
    ).toBeInTheDocument();
  });
});

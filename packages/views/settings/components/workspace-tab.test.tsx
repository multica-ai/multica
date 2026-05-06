import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

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

// Render the reset dialog as a tiny test stub: a button that calls onConfirm
// when the dialog is open. This bypasses the typed-confirmation friction
// (covered by reset-local-data-dialog.test.tsx) so we can focus on the
// workspace-tab wiring (button visibility, bridge call, toast).
vi.mock("./reset-local-data-dialog", () => ({
  ResetLocalDataDialog: ({
    open,
    onConfirm,
  }: {
    open: boolean;
    onConfirm: () => void;
  }) =>
    open ? (
      <button data-testid="reset-confirm" onClick={onConfirm}>
        Confirm reset (test stub)
      </button>
    ) : null,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
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

  it("renders Reset local data instead of Delete workspace when local capabilities apply", () => {
    // ownerMembers makes the user the owner — under non-local capabilities
    // this would surface the Delete-workspace destructive button. Under
    // local capabilities the swap kicks in.
    mockUseQuery.mockReturnValue({ data: ownerMembers, isFetched: true });

    render(
      <ProductCapabilitiesProvider capabilities={LOCAL_PRODUCT_CAPABILITIES}>
        <WorkspaceTab />
      </ProductCapabilitiesProvider>,
    );

    expect(
      screen.getByRole("button", { name: /reset local data/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /delete workspace/i }),
    ).toBeNull();
  });

  it("hides the Reset action when capabilities disable showResetLocalData", () => {
    mockUseQuery.mockReturnValue({ data: ownerMembers, isFetched: true });
    const cappedCapabilities: ProductCapabilities = {
      ...LOCAL_PRODUCT_CAPABILITIES,
      settings: {
        ...LOCAL_PRODUCT_CAPABILITIES.settings,
        showResetLocalData: false,
      },
    };

    render(
      <ProductCapabilitiesProvider capabilities={cappedCapabilities}>
        <WorkspaceTab />
      </ProductCapabilitiesProvider>,
    );

    expect(
      screen.queryByRole("button", { name: /reset local data/i }),
    ).toBeNull();
  });

  it("invokes the localResetAPI bridge when the user confirms the reset", async () => {
    mockUseQuery.mockReturnValue({ data: ownerMembers, isFetched: true });

    const reset = vi.fn().mockResolvedValue({ ok: true, removed: ["/x"] });
    Object.defineProperty(window, "localResetAPI", {
      configurable: true,
      value: { reset },
    });

    const user = userEvent.setup();
    render(
      <ProductCapabilitiesProvider capabilities={LOCAL_PRODUCT_CAPABILITIES}>
        <WorkspaceTab />
      </ProductCapabilitiesProvider>,
    );

    await user.click(
      screen.getByRole("button", { name: /reset local data/i }),
    );
    // The mocked dialog renders a confirm button while open.
    await user.click(screen.getByTestId("reset-confirm"));

    expect(reset).toHaveBeenCalledTimes(1);
  });
});

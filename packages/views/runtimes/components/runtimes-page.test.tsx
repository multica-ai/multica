// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  LOCAL_PRODUCT_CAPABILITIES,
  type ProductCapabilities,
} from "@multica/core/config";
import { ProductCapabilitiesProvider } from "@multica/core/platform";

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock("@multica/core/auth", () => {
  const stateUser = { id: "user-1", email: "u@example.com", name: "User" };
  const state = { user: stateUser, isLoading: false, isAuthenticated: true };
  const useAuthStore = (selector?: (s: typeof state) => unknown) =>
    selector ? selector(state) : state;
  return { useAuthStore };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId, "list"],
    queryFn: () => Promise.resolve([]),
  }),
  runtimeKeys: {
    all: (wsId: string) => ["runtimes", wsId],
  },
}));

vi.mock("@multica/core/runtimes/hooks", () => ({
  useUpdatableRuntimeIds: () => new Set<string>(),
}));

vi.mock("@multica/core/runtimes", () => ({
  deriveRuntimeHealth: () => "online",
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

// Stub child components so we can assert "not rendered".
vi.mock("./connect-remote-dialog", () => ({
  ConnectRemoteDialog: () => <div data-testid="connect-remote-dialog" />,
}));

vi.mock("./runtime-list", () => ({
  RuntimeList: () => <div data-testid="runtime-list" />,
}));

// ---------------------------------------------------------------------------
// Import component under test (after mocks)
// ---------------------------------------------------------------------------

import { RuntimesPage } from "./runtimes-page";

const cloudCaps: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  runtimes: {
    ...LOCAL_PRODUCT_CAPABILITIES.runtimes,
    allowRemoteConnection: true,
  },
};

describe("RuntimesPage capability gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides 'Connect remote machine' button in local mode (default capabilities)", () => {
    render(<RuntimesPage />);

    expect(
      screen.queryByRole("button", { name: /connect remote machine/i }),
    ).toBeNull();
    expect(screen.queryByTestId("connect-remote-dialog")).toBeNull();
  });

  it("renders 'Connect remote machine' button when allowRemoteConnection is true", () => {
    render(
      <ProductCapabilitiesProvider capabilities={cloudCaps}>
        <RuntimesPage />
      </ProductCapabilitiesProvider>,
    );

    const buttons = screen.getAllByRole("button", {
      name: /connect remote machine/i,
    });
    // Both the page header button AND the empty state button render when
    // remote connection is allowed and the runtime list is empty.
    expect(buttons.length).toBeGreaterThanOrEqual(1);
  });
});

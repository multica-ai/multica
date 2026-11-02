// @vitest-environment jsdom

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@multica/core/api";
import type { MessageEventCatalog, MessageSourceRoute } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";

import { PersonalFeishuPushSection } from "./personal-push-section";

const NAV_ADAPTER = {
  push: () => {},
  replace: () => {},
  back: () => {},
  pathname: "/ws/settings",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path: string) => path,
};

type QueryResult = {
  data?: unknown;
  isLoading: boolean;
  isError: boolean;
  isSuccess: boolean;
  error?: unknown;
};

const ok = vi.hoisted(
  () =>
    (data: unknown): QueryResult => ({
      data,
      isLoading: false,
      isError: false,
      isSuccess: true,
    }),
);

const routesRef = vi.hoisted(() => ({ current: ok([]) as QueryResult }));
const catalogRef = vi.hoisted(() => ({ current: ok(undefined) as QueryResult }));
const DEFAULT_INSTALLATIONS = {
  installations: [{ id: "inst-1", agent_id: "agent-1", status: "active", region: "feishu" }],
  configured: true,
};
const installationsRef = vi.hoisted(() => ({
  current: undefined as unknown as QueryResult,
}));

const mockCreate = vi.hoisted(() => vi.fn());
const mockUpdate = vi.hoisted(() => vi.fn());
const mockApprove = vi.hoisted(() => vi.fn());
const mockSetEnabled = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const mockTestSend = vi.hoisted(() => vi.fn());
const mockRetry = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) {
      return { data: undefined, isLoading: false, isError: false, isSuccess: false };
    }
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("catalog")) return catalogRef.current;
    if (key.includes("routes")) return routesRef.current;
    if (key.includes("installations")) return installationsRef.current;
    return { data: undefined, isLoading: false, isError: false, isSuccess: false };
  },
  useInfiniteQuery: () => ({
    data: { pages: [{ deliveries: [], limit: 100, offset: 0 }] },
    isLoading: false,
    isError: false,
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
  }),
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    fetchQuery: async () => ({ routes: routesRef.current.data ?? [] }),
  }),
  queryOptions: <T,>(opts: T) => opts,
  infiniteQueryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/message-delivery", () => ({
  messageSourceRoutesOptions: (_wsId: string, sourceKind?: string) => ({
    queryKey: ["message-sources", "ws-1", "routes", sourceKind ?? "all"],
  }),
  messageEventCatalogOptions: (_wsId: string) => ({
    queryKey: ["message-sources", "ws-1", "catalog"],
  }),
  messageRouteDeliveriesInfiniteOptions: (
    _wsId: string,
    routeId: string,
    scope?: { status?: string },
  ) => ({
    queryKey: ["message-sources", "ws-1", "deliveries", routeId, scope?.status ?? "all"],
  }),
  messageRouteDeliveryOptions: (_wsId: string, routeId: string, deliveryId: string) => ({
    queryKey: ["message-sources", "ws-1", "deliveries", routeId, "detail", deliveryId],
  }),
  messageSourceKeys: {
    routes: (wsId: string, sourceKind?: string) => [
      "message-sources",
      wsId,
      "routes",
      sourceKind ?? "all",
    ],
    delivery: (wsId: string, routeId: string, deliveryId: string) => [
      "message-sources",
      wsId,
      "deliveries",
      routeId,
      "detail",
      deliveryId,
    ],
  },
  useCreateMessageSourceRoute: () => ({ mutateAsync: mockCreate, isPending: false }),
  useUpdateMessageSourceRoute: () => ({ mutateAsync: mockUpdate, isPending: false }),
  useApproveMessageSourceTarget: () => ({ mutateAsync: mockApprove, isPending: false }),
  useSetMessageSourceRouteEnabled: () => ({ mutate: mockSetEnabled, isPending: false }),
  useDeleteMessageSourceRoute: () => ({ mutate: mockDelete, isPending: false }),
  useTestMessageSourceRoute: () => ({ mutate: mockTestSend, isPending: false }),
  useRetryMessageRouteDelivery: () => ({ mutate: mockRetry, isPending: false }),
}));

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: (wsId: string) => ({
    queryKey: ["lark", wsId, "installations"],
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_t: string, id: string) => `Agent ${id}` }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/ws/settings",
    issueDetail: (id: string) => `/ws/issues/${id}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

const CATALOG: MessageEventCatalog = {
  personal: {
    source_kind: "inbox",
    target_type: "member",
    event_types: [
      { type: "issue_assigned", group: "assignments", label: "Issue assigned to you" },
      { type: "new_comment", group: "comments", label: "New comment" },
    ],
  },
  team: [],
};

const PERSONAL_ROUTE: MessageSourceRoute = {
  id: "route-1",
  workspace_id: "ws-1",
  autopilot_id: null,
  source_kind: "inbox",
  installation_id: "inst-1",
  channel_type: "feishu",
  target_type: "member",
  target_user_id: "user-1",
  target_chat_id: null,
  target_message_id: null,
  target_thread_id: null,
  target_key: "member:user-1",
  project_id: null,
  event_types: [],
  enabled: true,
  revision: 2,
  created_by: "user-1",
  updated_by: "user-1",
  effective_from: "2026-09-08T02:30:00Z",
  last_disabled_at: null,
  created_at: "2026-09-08T02:30:00Z",
  updated_at: "2026-09-08T02:30:00Z",
};

function renderSection() {
  return renderWithI18n(
    <NavigationProvider value={NAV_ADAPTER}>
      <PersonalFeishuPushSection />
    </NavigationProvider>,
  );
}

describe("PersonalFeishuPushSection", () => {
  beforeEach(() => {
    routesRef.current = ok([]);
    catalogRef.current = ok(CATALOG);
    installationsRef.current = ok(DEFAULT_INSTALLATIONS);
    mockCreate.mockReset().mockResolvedValue({});
    mockUpdate.mockReset().mockResolvedValue({});
  });

  afterEach(() => {
    cleanup();
  });

  it("shows the restricted state on a real 403 (not your own inbox)", () => {
    routesRef.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      isSuccess: false,
      error: new ApiError("forbidden", 403, "Forbidden", { code: "route_not_self" }),
    };
    renderSection();
    expect(screen.getByText("You can only manage your own Feishu push.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /set up push/i })).not.toBeInTheDocument();
  });

  it("shows the unsupported state on 404 (server without OL-27)", () => {
    routesRef.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      isSuccess: false,
      error: new ApiError("not found", 404, "Not Found"),
    };
    renderSection();
    expect(
      screen.getByText(/doesn't support Feishu push yet/i),
    ).toBeInTheDocument();
  });

  it("shows the generic load failure on other errors", () => {
    routesRef.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      isSuccess: false,
      error: new ApiError("boom", 500, "Internal Server Error"),
    };
    renderSection();
    expect(screen.getByText("Couldn't load the push configuration.")).toBeInTheDocument();
  });

  it("renders a loading skeleton, not a phantom empty state", () => {
    routesRef.current = { data: undefined, isLoading: true, isError: false, isSuccess: false };
    renderSection();
    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
    expect(screen.queryByText(/isn't set up yet/i)).not.toBeInTheDocument();
  });

  it("shows the empty state with the add button when no route exists yet", () => {
    renderSection();
    expect(screen.getByText(/Feishu push isn't set up yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /set up push/i })).toBeEnabled();
  });

  it("points at integration settings and hides the add button when no active bot exists", () => {
    installationsRef.current = ok({ installations: [], configured: true });
    renderSection();
    expect(screen.getByText(/No active Feishu bot in this workspace/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /open integration settings/i })).toHaveAttribute(
      "href",
      "/ws/settings?tab=integrations&integration=lark",
    );
    expect(screen.queryByRole("button", { name: /set up push/i })).not.toBeInTheDocument();
  });

  it("renders the route row with the mute/system boundary captions", () => {
    routesRef.current = ok([PERSONAL_ROUTE]);
    renderSection();
    expect(screen.getByText("Your Feishu DM")).toBeInTheDocument();
    expect(screen.getByText("Agent agent-1")).toBeInTheDocument();
    expect(
      screen.getByText("Notification categories muted above are not forwarded to Feishu."),
    ).toBeInTheDocument();
    expect(screen.getByText(/don't turn Feishu push on or off/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /set up push/i })).toBeEnabled();
  });

  it("opens the personal editor from the add button", async () => {
    const user = userEvent.setup();
    renderSection();
    await user.click(screen.getByRole("button", { name: /set up push/i }));
    expect(await screen.findByText("Set up Feishu push")).toBeInTheDocument();
    // Personal mode: no target/source/scope pickers.
    expect(screen.queryByRole("combobox", { name: /^target$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: /^source$/i })).not.toBeInTheDocument();
  });

  it("opens the per-route records dialog from the row", async () => {
    routesRef.current = ok([PERSONAL_ROUTE]);
    const user = userEvent.setup();
    renderSection();
    await user.click(screen.getByRole("button", { name: /^records$/i }));
    expect(await screen.findByText("Deliveries for this rule")).toBeInTheDocument();
    expect(screen.getByText("No delivery records yet.")).toBeInTheDocument();
  });
});

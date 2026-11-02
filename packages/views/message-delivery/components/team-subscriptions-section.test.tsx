// @vitest-environment jsdom

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@multica/core/api";
import type {
  MessageEventCatalog,
  MessageSourceApprovedTarget,
  MessageSourceRoute,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";

import { TeamSubscriptionsSection } from "./team-subscriptions-section";

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
const approvalsRef = vi.hoisted(() => ({ current: ok([]) as QueryResult }));
const projectsRef = vi.hoisted(() => ({ current: ok([]) as QueryResult }));
const DEFAULT_INSTALLATIONS = {
  installations: [{ id: "inst-1", agent_id: "agent-1", status: "active", region: "feishu" }],
  configured: true,
};
const installationsRef = vi.hoisted(() => ({
  current: undefined as unknown as QueryResult,
}));
const roleRef = vi.hoisted(() => ({ current: "owner" as string | null }));
const roleLoadingRef = vi.hoisted(() => ({ current: false }));
// Every requested query key, so the non-admin gate can be verified by the
// absence of admin-gated requests — not just by what's rendered.
const requestedKeys = vi.hoisted(() => ({ current: [] as string[] }));

const mockCreate = vi.hoisted(() => vi.fn());
const mockUpdate = vi.hoisted(() => vi.fn());
const mockApprove = vi.hoisted(() => vi.fn());
const mockRevoke = vi.hoisted(() => vi.fn());
const mockSetEnabled = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const mockTestSend = vi.hoisted(() => vi.fn());
const mockRetry = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    const key = JSON.stringify(opts.queryKey);
    requestedKeys.current.push(key);
    if (opts.enabled === false) {
      return { data: undefined, isLoading: false, isError: false, isSuccess: false };
    }
    if (key.includes("approved-targets")) return approvalsRef.current;
    if (key.includes("catalog")) return catalogRef.current;
    if (key.includes("routes")) return routesRef.current;
    if (key.includes("installations")) return installationsRef.current;
    if (key.includes("projects")) return projectsRef.current;
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
  messageSourceApprovedTargetsOptions: (_wsId: string, options?: { enabled?: boolean }) => ({
    queryKey: ["message-sources", "ws-1", "approved-targets"],
    enabled: options?.enabled ?? true,
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
  useRevokeMessageSourceTarget: () => ({ mutate: mockRevoke, isPending: false }),
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

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: roleRef.current, isLoading: roleLoadingRef.current }),
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
  toast: { success: toastSuccess, error: toastError, warning: vi.fn() },
}));

// The Lark pickers' own behavior is covered by packages/views/lark/*.test.tsx;
// here they render their manual-entry fallback (the pre-discovery server path).
vi.mock("../../lark", () => ({
  LarkChatPicker: (props: { fallback: React.ReactNode }) => <>{props.fallback}</>,
  LarkAnchorPicker: (props: { fallback: React.ReactNode }) => <>{props.fallback}</>,
}));


const CATALOG: MessageEventCatalog = {
  personal: { source_kind: "inbox", target_type: "member", event_types: [] },
  team: [
    {
      source_kind: "activity",
      events: [{ event: "status_changed", label: "Issue status changed" }],
    },
  ],
};

const PROJECTS = [{ id: "p1", title: "Alpha" }];

const INBOX_ROUTE: MessageSourceRoute = {
  id: "route-inbox",
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
  revision: 1,
  created_by: "user-1",
  updated_by: "user-1",
  effective_from: "2026-09-08T02:30:00Z",
  last_disabled_at: null,
  created_at: "2026-09-08T02:30:00Z",
  updated_at: "2026-09-08T02:30:00Z",
};

const ACTIVITY_ROUTE: MessageSourceRoute = {
  ...INBOX_ROUTE,
  id: "route-activity",
  source_kind: "activity",
  target_type: "group",
  target_user_id: null,
  target_chat_id: "oc_1",
  target_key: "group:oc_1",
  project_id: "p1",
  event_types: ["status_changed"],
  revision: 3,
};

const APPROVAL: MessageSourceApprovedTarget = {
  id: "t1",
  workspace_id: "ws-1",
  autopilot_id: null,
  source_kind: "activity",
  project_id: "p1",
  installation_id: "inst-1",
  target_key: "group:oc_1",
  target_type: "group",
  approved_by: "user-1",
  approved_at: "2026-09-08T02:30:00Z",
  revoked_at: null,
};

function renderSection() {
  return renderWithI18n(
    <NavigationProvider value={NAV_ADAPTER}>
      <TeamSubscriptionsSection />
    </NavigationProvider>,
  );
}

describe("TeamSubscriptionsSection", () => {
  beforeEach(() => {
    routesRef.current = ok([]);
    catalogRef.current = ok(CATALOG);
    approvalsRef.current = ok([]);
    projectsRef.current = ok(PROJECTS);
    installationsRef.current = ok(DEFAULT_INSTALLATIONS);
    roleRef.current = "owner";
    roleLoadingRef.current = false;
    requestedKeys.current = [];
    mockCreate.mockReset().mockResolvedValue({});
    mockUpdate.mockReset().mockResolvedValue({});
    mockApprove.mockReset().mockResolvedValue({});
    mockRevoke.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders a skeleton while the current member's role is loading", () => {
    roleLoadingRef.current = true;
    renderSection();
    expect(screen.getByText("Team event subscriptions")).toBeInTheDocument();
    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
    expect(screen.queryByText(/configured by workspace owners\/admins/i)).not.toBeInTheDocument();
  });

  it("gates plain members out without firing any admin-gated query", () => {
    roleRef.current = "member";
    renderSection();
    expect(screen.getByText("Team event subscriptions")).toBeInTheDocument();
    expect(
      screen.getByText(/configured by workspace owners\/admins/i),
    ).toBeInTheDocument();
    // The body never mounts for non-admins: no routes, catalog, installations,
    // projects or approvals request may fire.
    expect(requestedKeys.current).toEqual([]);
    expect(screen.queryByRole("button", { name: /add subscription/i })).not.toBeInTheDocument();
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
    expect(screen.getByText(/doesn't support team subscriptions yet/i)).toBeInTheDocument();
  });

  it("shows the load failure on other errors", () => {
    routesRef.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      isSuccess: false,
      error: new ApiError("boom", 500, "Internal Server Error"),
    };
    renderSection();
    expect(screen.getByText("Couldn't load team subscriptions.")).toBeInTheDocument();
  });

  it("shows the empty state with the add button for admins", () => {
    renderSection();
    expect(screen.getByText("No team subscriptions yet.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add subscription/i })).toBeEnabled();
  });

  it("shows the no-bot variant and hides the add button without an active bot", () => {
    installationsRef.current = ok({ installations: [], configured: true });
    renderSection();
    expect(
      screen.getByText("No active Feishu bot in this workspace. Install one above first."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add subscription/i })).not.toBeInTheDocument();
  });

  it("lists only team routes with source-kind and project badges", () => {
    routesRef.current = ok([INBOX_ROUTE, ACTIVITY_ROUTE]);
    renderSection();
    // The admin's own inbox rule belongs to the personal settings, not here.
    expect(screen.queryByText("Your Feishu DM")).not.toBeInTheDocument();
    expect(screen.getByText("oc_1")).toBeInTheDocument();
    expect(screen.getByText("Task activity")).toBeInTheDocument();
    expect(screen.getByText("Alpha")).toBeInTheDocument();
  });

  it("opens the team editor from the add button", async () => {
    const user = userEvent.setup();
    renderSection();
    await user.click(screen.getByRole("button", { name: /add subscription/i }));
    // Team mode: the source-kind picker is present (absent in personal mode).
    expect(await screen.findByRole("combobox", { name: /^source$/i })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: /^scope$/i })).toBeInTheDocument();
  });

  it("lists approved targets and revokes one behind the confirm dialog", async () => {
    routesRef.current = ok([ACTIVITY_ROUTE]);
    approvalsRef.current = ok([APPROVAL]);
    mockRevoke.mockImplementation(
      (_vars: unknown, opts: { onSuccess?: (r: { cancelled_deliveries: number }) => void }) =>
        opts.onSuccess?.({ cancelled_deliveries: 2 }),
    );
    const user = userEvent.setup();
    renderSection();

    expect(screen.getByText("Approved team targets")).toBeInTheDocument();
    expect(screen.getByText("group:oc_1")).toBeInTheDocument();
    expect(screen.getByText("Group chat")).toBeInTheDocument();
    // The approval's exact scope: source kind + project range badges.
    expect(screen.getAllByText("Task activity").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Alpha").length).toBeGreaterThan(0);
    // The "Approved <Month> <d>, …" caption (uppercase month distinguishes it
    // from the "Approved team targets" heading).
    expect(screen.getByText(/^Approved [A-Z][a-z]{2} \d{1,2},/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^revoke$/i }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Revoke this approval?")).toBeInTheDocument();
    expect(mockRevoke).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: /^revoke$/i }));
    expect(mockRevoke).toHaveBeenCalledWith({ targetId: "t1" }, expect.anything());
    // The success toast reports how many queued deliveries were cancelled.
    expect(toastSuccess).toHaveBeenCalledWith("Approval revoked, 2 queued delivery(s) cancelled");
  });

  it("shows the approvals empty note when there are none", () => {
    renderSection();
    expect(screen.getByText("Approved team targets")).toBeInTheDocument();
    expect(screen.getByText("No approved team targets yet.")).toBeInTheDocument();
  });
});

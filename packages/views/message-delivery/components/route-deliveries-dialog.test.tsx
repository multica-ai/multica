// @vitest-environment jsdom

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@multica/core/api";
import type {
  GetMessageRouteDeliveryResponse,
  MessageSourceDelivery,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";

import { RouteDeliveriesDialog, SourceDeliveryDetailDialog } from "./route-deliveries-dialog";

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

type InfiniteResult = {
  data?: { pages: { deliveries: MessageSourceDelivery[]; limit: number; offset: number }[] };
  isLoading: boolean;
  isError: boolean;
  error?: unknown;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
};

const emptyPage = vi.hoisted(
  () =>
    (): InfiniteResult => ({
      data: { pages: [{ deliveries: [], limit: 100, offset: 0 }] },
      isLoading: false,
      isError: false,
      hasNextPage: false,
      isFetchingNextPage: false,
    }),
);

// The infinite query dispatches on the stringified key (which carries the
// status filter) so a filter change is observable as a scope change.
const infiniteHandler = vi.hoisted(() => ({ current: emptyPage() as InfiniteResult }));
const infiniteKeys = vi.hoisted(() => ({ current: [] as string[] }));
const fetchNextPageSpy = vi.hoisted(() => vi.fn());
const detailRef = vi.hoisted(() => ({ current: ok(null) as QueryResult }));
const membersRef = vi.hoisted(() => ({ current: ok([]) as QueryResult }));

const mockRetry = vi.hoisted(() => vi.fn());
const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    const key = JSON.stringify(opts.queryKey);
    infiniteKeys.current.push(key);
    return { ...infiniteHandler.current, fetchNextPage: fetchNextPageSpy };
  },
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) {
      return { data: undefined, isLoading: false, isError: false, isSuccess: false };
    }
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("detail")) return detailRef.current;
    if (key.includes("members")) return membersRef.current;
    return { data: undefined, isLoading: false, isError: false, isSuccess: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  queryOptions: <T,>(opts: T) => opts,
  infiniteQueryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/message-delivery", () => ({
  messageRouteDeliveriesInfiniteOptions: (
    _wsId: string,
    routeId: string,
    scope?: { status?: string },
    options?: { enabled?: boolean },
  ) => ({
    queryKey: ["message-sources", "ws-1", "deliveries", routeId, scope?.status ?? "all"],
    enabled: options?.enabled ?? true,
  }),
  messageRouteDeliveryOptions: (
    _wsId: string,
    routeId: string,
    deliveryId: string,
    options?: { enabled?: boolean },
  ) => ({
    queryKey: ["message-sources", "ws-1", "deliveries", routeId, "detail", deliveryId],
    enabled: options?.enabled ?? true,
  }),
  messageSourceKeys: {
    delivery: (wsId: string, routeId: string, deliveryId: string) => [
      "message-sources",
      wsId,
      "deliveries",
      routeId,
      "detail",
      deliveryId,
    ],
  },
  useRetryMessageRouteDelivery: () => ({ mutate: mockRetry, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/ws/settings",
    issueDetail: (id: string) => `/ws/issues/${id}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError, warning: vi.fn() },
}));

function delivery(id: string, overrides: Partial<MessageSourceDelivery> = {}): MessageSourceDelivery {
  return {
    id,
    workspace_id: "ws-1",
    route_id: "route-1",
    route_revision: 2,
    autopilot_id: null,
    run_id: null,
    source_ref_id: "inbox-1",
    requested_by: null,
    source_kind: "inbox",
    source_scope: "inbox",
    source_project_id: null,
    status: "sent",
    attempts: 1,
    next_attempt_at: null,
    error_code: null,
    last_error: null,
    shard_total: 1,
    installation_id: "inst-1",
    target_key: "member:user-1",
    delivered_at: "2026-09-08T02:31:00Z",
    first_attempt_at: "2026-09-08T02:30:05Z",
    created_at: "2026-09-08T02:30:01Z",
    updated_at: "2026-09-08T02:31:00Z",
    ...overrides,
  };
}

function pageWith(rows: MessageSourceDelivery[], hasNextPage = false): InfiniteResult {
  return {
    data: { pages: [{ deliveries: rows, limit: 100, offset: 0 }] },
    isLoading: false,
    isError: false,
    hasNextPage,
    isFetchingNextPage: false,
  };
}

function renderList() {
  return renderWithI18n(
    <NavigationProvider value={NAV_ADAPTER}>
      <RouteDeliveriesDialog open onOpenChange={() => {}} routeId="route-1" canManage />
    </NavigationProvider>,
  );
}

function renderDetail(deliveryProp: MessageSourceDelivery, canManage = true) {
  return renderWithI18n(
    <NavigationProvider value={NAV_ADAPTER}>
      <SourceDeliveryDetailDialog
        open
        onOpenChange={() => {}}
        routeId="route-1"
        delivery={deliveryProp}
        canManage={canManage}
      />
    </NavigationProvider>,
  );
}

describe("RouteDeliveriesDialog", () => {
  beforeEach(() => {
    infiniteHandler.current = emptyPage();
    infiniteKeys.current = [];
    fetchNextPageSpy.mockReset();
    detailRef.current = ok(null);
    membersRef.current = ok([]);
    mockRetry.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("shows the empty state only on a successful zero-row page", () => {
    renderList();
    expect(screen.getByText("No delivery records yet.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });

  it("renders skeletons while loading — never the empty state", () => {
    infiniteHandler.current = {
      data: undefined,
      isLoading: true,
      isError: false,
      hasNextPage: false,
      isFetchingNextPage: false,
    };
    // Dialog content portals out of the render container — query the document.
    renderList();
    expect(document.querySelectorAll('[data-slot="skeleton"]').length).toBeGreaterThan(0);
    expect(screen.queryByText("No delivery records yet.")).not.toBeInTheDocument();
  });

  it("shows the query error as an alert, not as an empty list", () => {
    infiniteHandler.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      error: new ApiError("forbidden", 403, "Forbidden", { code: "message_forbidden" }),
      hasNextPage: false,
      isFetchingNextPage: false,
    };
    renderList();
    expect(
      screen.getByText("You don't have access to this notification configuration."),
    ).toBeInTheDocument();
    expect(screen.queryByText("No delivery records yet.")).not.toBeInTheDocument();
  });

  it("changing the status filter re-scopes the infinite query", async () => {
    renderList();
    const user = userEvent.setup();

    expect(infiniteKeys.current.some((k) => k.includes('"all"'))).toBe(true);

    await user.click(screen.getByRole("combobox", { name: "Delivery records" }));
    await user.click(await screen.findByRole("option", { name: "Failed" }));

    await waitFor(() =>
      expect(infiniteKeys.current.some((k) => k.includes('"failed"'))).toBe(true),
    );
  });

  it("fetches the next page from the load-more button", async () => {
    infiniteHandler.current = pageWith([delivery("d-1")], true);
    renderList();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /load more/i }));
    expect(fetchNextPageSpy).toHaveBeenCalledTimes(1);
  });

  it("opens the delivery detail from a row", async () => {
    infiniteHandler.current = pageWith([delivery("d-1")]);
    detailRef.current = ok({
      delivery: delivery("d-1"),
      content_snapshot: { summary: "Alice mentioned you" },
      target_snapshot: { target_type: "member", user_id: "user-1" },
      source_ref: null,
      receipts: [],
    } satisfies GetMessageRouteDeliveryResponse);
    renderList();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /member:user-1/i }));
    expect(await screen.findByText("Delivery detail")).toBeInTheDocument();
    expect(screen.getByText("Alice mentioned you")).toBeInTheDocument();
  });
});

describe("SourceDeliveryDetailDialog", () => {
  beforeEach(() => {
    infiniteKeys.current = [];
    detailRef.current = ok(null);
    membersRef.current = ok([
      { user_id: "user-1", name: "Alice", email: "alice@example.com" },
    ]);
    mockRetry.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  const FAILED = delivery("d-1", {
    status: "failed",
    source_kind: "comment",
    source_scope: "comment",
    error_code: "send_rejected",
    last_error: "HTTP 400: bad card",
  });

  function stubDetail(overrides: Partial<GetMessageRouteDeliveryResponse> = {}) {
    detailRef.current = ok({
      delivery: FAILED,
      content_snapshot: null,
      target_snapshot: null,
      source_ref: null,
      receipts: [],
      ...overrides,
    } satisfies GetMessageRouteDeliveryResponse);
  }

  it("shows the frozen content snapshot, receipts and the issue locator", () => {
    stubDetail({
      content_snapshot: {
        summary: "Alice commented",
        change: "Status: In Progress → Done",
        text: "the full comment body",
      },
      target_snapshot: { target_type: "group", chat_id: "oc_1" },
      source_ref: { source_kind: "comment", issue_identifier: "OL-28" },
      receipts: [
        {
          id: "r-1",
          delivery_id: "d-1",
          workspace_id: "ws-1",
          installation_id: "inst-1",
          shard_index: 0,
          shard_total: 2,
          send_uuid: "b2f7d1e4-0000-4000-8000-000000000000",
          external_message_id: "om_abc",
          created_at: "2026-09-08T02:30:05Z",
          updated_at: "2026-09-08T02:30:05Z",
        },
      ],
    });
    renderDetail(FAILED);

    expect(screen.getByText("Alice commented")).toBeInTheDocument();
    expect(screen.getByText("Status: In Progress → Done")).toBeInTheDocument();
    expect(screen.getByText("the full comment body")).toBeInTheDocument();
    // The frozen failure reason, not a live lookup.
    expect(screen.getByText("Feishu rejected the message")).toBeInTheDocument();
    expect(screen.getByText("HTTP 400: bad card")).toBeInTheDocument();
    // Receipt ledger.
    expect(screen.getByText("Receipts")).toBeInTheDocument();
    expect(screen.getByText("Shard 1/2")).toBeInTheDocument();
    expect(screen.getByText("om_abc")).toBeInTheDocument();
    // The source-record locator links to the issue, never grants it.
    expect(screen.getByRole("link", { name: /view task OL-28/i })).toHaveAttribute(
      "href",
      "/ws/issues/OL-28",
    );
    // Group target renders the frozen chat id.
    expect(screen.getByText("oc_1")).toBeInTheDocument();
  });

  it("resolves a member target to the member's name", () => {
    stubDetail({
      target_snapshot: { target_type: "member", user_id: "user-1" },
    });
    renderDetail(delivery("d-1", { status: "failed" }));
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("retries a failed delivery directly when canManage", async () => {
    mockRetry.mockImplementation(
      (_vars: unknown, opts: { onSuccess?: () => void }) => opts.onSuccess?.(),
    );
    stubDetail();
    renderDetail(FAILED);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(mockRetry).toHaveBeenCalledWith(
      { routeId: "route-1", deliveryId: "d-1" },
      expect.anything(),
    );
    expect(toastSuccess).toHaveBeenCalledWith("Delivery re-queued");
    // A failed send needs no verify-first gate.
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("gates an uncertain retry behind the verify-first dialog", async () => {
    mockRetry.mockImplementation(
      (_vars: unknown, opts: { onSuccess?: () => void }) => opts.onSuccess?.(),
    );
    const uncertain = delivery("d-1", { status: "uncertain" });
    stubDetail({ delivery: uncertain });
    renderDetail(uncertain);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: /^retry$/i }));

    // The message may already have reached Feishu: no retry before the
    // explicit verification confirm.
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Verify before retrying")).toBeInTheDocument();
    expect(mockRetry).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: /checked — retry/i }));
    expect(mockRetry).toHaveBeenCalledWith(
      { routeId: "route-1", deliveryId: "d-1" },
      expect.anything(),
    );
  });

  it("offers no retry for a terminal sent delivery", () => {
    const sent = delivery("d-1", { status: "sent" });
    stubDetail({ delivery: sent });
    renderDetail(sent);

    expect(screen.getByRole("button", { name: /^retry$/i })).toBeDisabled();
    expect(
      screen.getByText("Only failed or uncertain deliveries can be retried."),
    ).toBeInTheDocument();
  });

  it("offers no retry without manage permission, and says why", () => {
    stubDetail();
    renderDetail(FAILED, false);

    expect(screen.getByRole("button", { name: /^retry$/i })).toBeDisabled();
    expect(screen.getByText(/read-only access/i)).toBeInTheDocument();
  });

  it("treats a failed detail read as an explicit error, never an empty message", () => {
    detailRef.current = {
      data: undefined,
      isLoading: false,
      isError: true,
      isSuccess: false,
      error: new ApiError("gone", 404, "Not Found", { code: "delivery_not_found" }),
    };
    renderDetail(FAILED);

    expect(screen.getByText("This delivery no longer exists.")).toBeInTheDocument();
    expect(screen.getByText(/the detail failed to load/i)).toBeInTheDocument();
    // Retry stays unavailable until the real state is known.
    expect(screen.getByRole("button", { name: /^retry$/i })).toBeDisabled();
  });
});

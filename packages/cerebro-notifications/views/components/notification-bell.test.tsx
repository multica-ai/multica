import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { InboxItem } from "@multica/core/types";

const { state, markReadMutate, markAllMutate, pushMock } = vi.hoisted(() => ({
  state: {
    items: [] as InboxItem[],
    unread: 0,
  },
  markReadMutate: vi.fn(),
  markAllMutate: vi.fn(),
  pushMock: vi.fn(),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({
    children,
    ...rest
  }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...rest}>
      {children}
    </button>
  ),
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    disabled,
    ...rest
  }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" onClick={onClick} disabled={disabled} {...rest}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...args: unknown[]) => args.filter(Boolean).join(" "),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    notifications: () => "/acme/notifications",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("@multica/views/common/actor-avatar", () => ({
  ActorAvatar: ({ actorType }: { actorType: string }) => (
    <span data-testid="actor-avatar" data-actor={actorType} />
  ),
}));

vi.mock("@multica/views/navigation", () => ({
  AppLink: ({
    children,
    href,
    onClick,
    ...rest
  }: React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={href} onClick={onClick} {...rest}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: pushMock }),
}));

vi.mock("@multica/views/inbox/components/inbox-list-item", () => ({
  useTimeAgo: () => (_dateStr: string) => "now",
}));

vi.mock("@multica/cerebro-notifications/core", () => ({
  notificationsListOptions: (wsId: string) => ({
    queryKey: ["notifications", wsId, "list"],
  }),
  notificationsUnreadCountOptions: (wsId: string) => ({
    queryKey: ["notifications", wsId, "unread-count"],
  }),
  useMarkNotificationRead: () => ({
    mutate: markReadMutate,
    isPending: false,
  }),
  useMarkAllNotificationsRead: () => ({
    mutate: markAllMutate,
    isPending: false,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[2] === "unread-count")
      return { data: { count: state.unread } };
    if (queryKey[2] === "list") return { data: state.items };
    return { data: undefined };
  },
}));

// Import AFTER mocks are registered so the component picks them up.
import { NotificationBell } from "./notification-bell";

const baseItem: InboxItem = {
  id: "n1",
  workspace_id: "ws-1",
  recipient_type: "member",
  recipient_id: "u1",
  actor_type: "member",
  actor_id: "u2",
  type: "mentioned",
  severity: "info",
  route: "notifications",
  issue_id: "iss-1",
  project_id: null,
  title: "Alice mentioned you",
  body: "in MUL-42",
  issue_status: null,
  read: false,
  archived: false,
  muted_until: null,
  created_at: "2026-05-19T15:00:00Z",
  details: null,
};

function makeItem(over: Partial<InboxItem>, id: string): InboxItem {
  return { ...baseItem, ...over, id };
}

beforeEach(() => {
  state.items = [];
  state.unread = 0;
  markReadMutate.mockReset();
  markAllMutate.mockReset();
  pushMock.mockReset();
});

describe("NotificationBell", () => {
  it("hides the badge when there are no unread notifications", () => {
    state.unread = 0;
    render(<NotificationBell />);
    expect(
      screen.queryByTestId("notification-bell-badge"),
    ).not.toBeInTheDocument();
  });

  it("shows the unread count on the badge", () => {
    state.unread = 7;
    render(<NotificationBell />);
    expect(screen.getByTestId("notification-bell-badge")).toHaveTextContent(
      "7",
    );
  });

  it("caps the badge label at 99+", () => {
    state.unread = 250;
    render(<NotificationBell />);
    expect(screen.getByTestId("notification-bell-badge")).toHaveTextContent(
      "99+",
    );
  });

  it("renders an empty state when there are no items", () => {
    state.items = [];
    render(<NotificationBell />);
    expect(screen.getByText(/all caught up/i)).toBeInTheDocument();
  });

  it("renders up to ten notifications and drops the rest", () => {
    state.items = Array.from({ length: 12 }, (_, i) =>
      makeItem({ title: `Notification ${i}` }, `n${i}`),
    );
    render(<NotificationBell />);
    const rows = screen.getAllByTestId("notification-bell-row");
    expect(rows).toHaveLength(10);
    expect(screen.queryByText("Notification 10")).not.toBeInTheDocument();
  });

  it("hides archived items in the dropdown list", () => {
    state.items = [
      makeItem({ title: "Visible" }, "n1"),
      makeItem({ title: "Archived", archived: true }, "n2"),
    ];
    render(<NotificationBell />);
    expect(screen.getByText("Visible")).toBeInTheDocument();
    expect(screen.queryByText("Archived")).not.toBeInTheDocument();
  });

  it("marks an unread row as read and navigates to its issue when clicked", () => {
    state.items = [makeItem({}, "n1")];
    render(<NotificationBell />);
    fireEvent.click(screen.getByTestId("notification-bell-row"));
    expect(markReadMutate).toHaveBeenCalledWith("n1");
    expect(pushMock).toHaveBeenCalledWith("/acme/issues/iss-1");
  });

  it("does not call mark-read when the row is already read", () => {
    state.items = [makeItem({ read: true }, "n1")];
    render(<NotificationBell />);
    fireEvent.click(screen.getByTestId("notification-bell-row"));
    expect(markReadMutate).not.toHaveBeenCalled();
    expect(pushMock).toHaveBeenCalledWith("/acme/issues/iss-1");
  });

  it("triggers mark-all-read when the header button is clicked", () => {
    state.unread = 3;
    state.items = [makeItem({}, "n1")];
    render(<NotificationBell />);
    fireEvent.click(screen.getByTestId("notification-bell-mark-all-read"));
    expect(markAllMutate).toHaveBeenCalledTimes(1);
  });

  it("disables mark-all-read when there is nothing to clear", () => {
    state.unread = 0;
    state.items = [makeItem({ read: true }, "n1")];
    render(<NotificationBell />);
    expect(screen.getByTestId("notification-bell-mark-all-read")).toBeDisabled();
  });

  it("links 'See all' to the notifications page", () => {
    state.unread = 1;
    state.items = [makeItem({}, "n1")];
    render(<NotificationBell />);
    expect(screen.getByTestId("notification-bell-see-all")).toHaveAttribute(
      "href",
      "/acme/notifications",
    );
  });

  it("tags rows with agent actor type so the dropdown can distinguish them visually", () => {
    state.items = [
      makeItem({ actor_type: "agent", actor_id: "agent-1" }, "n1"),
      makeItem({ actor_type: "member" }, "n2"),
      makeItem(
        { actor_type: "system", actor_id: null, recipient_type: "member" },
        "n3",
      ),
    ];
    render(<NotificationBell />);
    const rows = screen.getAllByTestId("notification-bell-row");
    expect(rows[0]).toHaveAttribute("data-actor-type", "agent");
    expect(rows[1]).toHaveAttribute("data-actor-type", "member");
    expect(rows[2]).toHaveAttribute("data-actor-type", "agent");
  });
});

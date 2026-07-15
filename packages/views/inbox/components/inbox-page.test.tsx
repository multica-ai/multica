import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { InboxPage } from "./inbox-page";

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isLoading: false }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: vi.fn() }) },
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: { getState: () => ({ setDraft: vi.fn() }) },
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxListOptions: () => ({ queryKey: ["inbox"] }),
  deduplicateInboxItems: (items: unknown[]) => items,
  useInboxUnreadCount: () => 2,
}));

vi.mock("@multica/core/inbox/mutations", () => {
  const mutation = () => ({ mutate: vi.fn() });
  return {
    useMarkInboxRead: mutation,
    useArchiveInbox: mutation,
    useMarkAllInboxRead: mutation,
    useArchiveAllInbox: mutation,
    useArchiveAllReadInbox: mutation,
    useArchiveCompletedInbox: mutation,
  };
});

vi.mock("../../issues/components", () => ({ IssueDetail: () => null }));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams: new URLSearchParams(), replace: vi.fn() }),
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => true }));
vi.mock("./inbox-list", () => ({ InboxList: () => null }));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => vi.fn() }));
vi.mock("./inbox-detail-label", () => ({ useTypeLabels: () => ({}) }));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Inbox" }) }));

describe("InboxPage", () => {
  it("keeps the title unread count static", () => {
    const { container } = render(<InboxPage />);
    const titleCount = container.querySelector("h1")?.parentElement?.querySelector(
      "number-flow-react",
    ) as (HTMLElement & { animated?: boolean }) | null;

    expect(titleCount?.getAttribute("aria-label")).toBe("2");
    expect(titleCount?.animated).toBe(false);
  });
});

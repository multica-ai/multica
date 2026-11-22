// @vitest-environment node
import type { InboxItem } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { router } from "expo-router";
import Inbox from "@/app/(app)/[workspace]/(tabs)/inbox";

const state = vi.hoisted(() => ({ rows: [] as InboxItem[], loading: false, markRead: vi.fn() }));
// useState slots kept across calls of the screen function, so a test can
// render it again as the query result changes.
const hooks = vi.hoisted(() => ({ slots: [] as unknown[], cursor: 0, updated: false }));
vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useMemo: (create: () => unknown) => create(),
  useCallback: (callback: unknown) => callback,
  useState: (initial: unknown) => {
    const slot = hooks.cursor++;
    if (!(slot in hooks.slots)) hooks.slots[slot] = initial;
    return [hooks.slots[slot], (next: unknown) => {
      hooks.slots[slot] = next;
      hooks.updated = true;
    }];
  },
}));
vi.mock("react-native", () => ({
  ActionSheetIOS: {}, Alert: {}, FlatList: "FlatList", View: "View",
}));
vi.mock("expo-router", () => ({ router: { push: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: state.loading ? undefined : state.rows,
    isLoading: state.loading,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/data/queries/inbox", () => ({ inboxListOptions: vi.fn() }));
vi.mock("@/data/mutations/inbox", () => {
  const mutation = () => ({ mutate: vi.fn() });
  return {
    useMarkInboxRead: () => ({ mutate: state.markRead }),
    useMarkAllInboxRead: mutation,
    useArchiveInbox: mutation,
    useArchiveAllInbox: mutation,
    useArchiveAllReadInbox: mutation,
    useArchiveCompletedInbox: mutation,
  };
});
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (value: unknown) => unknown) => selector({
    currentWorkspaceId: "workspace-1", currentWorkspaceSlug: "acme",
  }),
}));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/components/ui/button", () => ({ Button: "Button" }));
vi.mock("@/components/ui/skeleton", () => ({ Skeleton: "Skeleton" }));
vi.mock("@/components/ui/header", () => ({ Header: "Header" }));
vi.mock("@/components/ui/icon-button", () => ({ IconButton: "IconButton" }));
vi.mock("@/components/ui/app-header-actions", () => ({ HeaderActions: "HeaderActions" }));
vi.mock("@/components/inbox/swipeable-inbox-row", () => ({ SwipeableInboxRow: "SwipeableInboxRow" }));
vi.mock("@/components/inbox/inbox-zero", () => ({ InboxZero: "InboxZero" }));

/** Calls the screen like React does, again while a render-phase update is pending. */
function render() {
  let tree;
  do {
    hooks.updated = false;
    hooks.cursor = 0;
    tree = Inbox();
  } while (hooks.updated);
  return tree;
}

const comment: InboxItem = {
  id: "comment", workspace_id: "workspace-1", recipient_type: "member",
  recipient_id: "member-1", actor_type: "agent", actor_id: "agent-1",
  type: "new_comment", severity: "info", issue_id: "issue-1", title: "Issue",
  body: null, issue_status: "todo", read: false, archived: false,
  created_at: "2026-06-15T08:01:00Z", details: null,
};

beforeEach(() => {
  vi.clearAllMocks();
  hooks.slots = [];
  state.loading = false;
});

describe("Inbox navigation wiring", () => {
  it.each([true, false])("reads the whole issue before navigation when the latest row read=%s", (read) => {
    const latest = { ...comment, read };
    state.rows = [latest, {
      ...comment, id: "assigned", type: "issue_assigned", severity: "action_required",
      read: false, created_at: "2026-06-15T08:00:00Z",
    }];

    // Inspect props and invoke the tap callback without rendering native UI.
    const list = render().props.children[1];
    expect(list.props.data).toEqual([latest]);
    list.props.renderItem({ item: latest }).props.onPress();

    expect(state.markRead).toHaveBeenCalledExactlyOnceWith("comment");
    expect(router.push).toHaveBeenCalledWith(expect.objectContaining({
      pathname: "/[workspace]/issue/[id]",
      params: expect.objectContaining({ workspace: "acme", id: "issue-1" }),
    }));
    expect(state.markRead.mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(router.push).mock.invocationCallOrder[0]!);
  });
});

describe("Inbox Zero", () => {
  const zero = () => render().props.children[1].props.ListEmptyComponent;

  it("keeps an Inbox that opens empty a pull-to-refresh list, at rest", () => {
    state.rows = [];
    const list = render().props.children[1];
    expect(list.type).toBe("FlatList");
    expect(list.props.onRefresh).toEqual(expect.any(Function));
    expect(zero()).toMatchObject({ type: "InboxZero", props: { rises: false } });
  });

  it("rises when the last notification leaves", () => {
    state.rows = [comment];
    render();
    state.rows = [{ ...comment, archived: true }];
    expect(zero().props.rises).toBe(true);
  });

  it("stays at rest when the Inbox reloads empty, as after a workspace switch", () => {
    state.rows = [comment];
    render();
    state.loading = true;
    render();
    state.loading = false;
    state.rows = [];
    expect(zero().props.rises).toBe(false);
  });
});

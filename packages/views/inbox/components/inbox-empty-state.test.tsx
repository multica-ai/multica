import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import { inboxKeys, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import type { SnoozedInboxIssue, WorkspaceWorkingAgent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { InboxEmptyState } from "./inbox-empty-state";
import { hasInboxUndo, takeInboxUndo, toastInboxUndo } from "./inbox-undo-toast";

const spies = vi.hoisted(() => ({
  restoreInbox: vi.fn((..._args: unknown[]) => Promise.resolve(true)),
  unsnoozeAsync: vi.fn(() => Promise.resolve()),
  undoUnsubscribe: vi.fn((..._args: unknown[]) => Promise.resolve()),
  undoBatch: vi.fn((..._args: unknown[]) => Promise.resolve()),
  guest: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => spies.guest }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/inbox/mutations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/mutations")>()),
  useUnsnoozeInbox: () => ({ mutateAsync: spies.unsnoozeAsync }),
  undoUnsubscribeArchiveInbox: (...args: unknown[]) => spies.undoUnsubscribe(...args),
  undoArchiveInboxBatch: (...args: unknown[]) => spies.undoBatch(...args),
}));
vi.mock("./use-leave-inbox-row", () => ({
  useRestoreInbox: () => spies.restoreInbox,
}));
vi.mock("sonner", () => {
  let next = 0;
  return { toast: { success: vi.fn(() => ++next), dismiss: vi.fn(), error: vi.fn() } };
});

const archivedEntry = {
  kind: "archived" as const,
  itemId: "n-1",
  key: "issue-1",
  label: "MUL-9790",
  href: "/acme/issues/MUL-9790",
  clearedAt: Date.now(),
};
const pressZ = (target: Element = document.body) => fireEvent.keyDown(target, { key: "z", code: "KeyZ" });

function renderEmpty({
  isArchivedView = false,
  visibleCount = 0,
  totalActiveCount = 0,
  hasActiveFilters = false,
  inboxZeroRises = false,
  isCompact = false,
  snoozed = [],
  workingAgents = [],
  onClearFiltersAndContinue = vi.fn(),
  onViewArchived = vi.fn(),
}: {
  isArchivedView?: boolean;
  visibleCount?: number;
  totalActiveCount?: number;
  hasActiveFilters?: boolean;
  inboxZeroRises?: boolean;
  isCompact?: boolean;
  snoozed?: SnoozedInboxIssue[];
  workingAgents?: WorkspaceWorkingAgent[];
  onClearFiltersAndContinue?: () => void;
  onViewArchived?: () => void;
} = {}) {
  const nav: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/inbox",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(snoozedInboxOptions("ws").queryKey, snoozed);
  qc.setQueryData(workspaceWorkingAgentsOptions("ws", "issue").queryKey, workingAgents);
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={nav}>
        <InboxEmptyState
          wsId="ws"
          isArchivedView={isArchivedView}
          visibleCount={visibleCount}
          totalActiveCount={totalActiveCount}
          hasActiveFilters={hasActiveFilters}
          inboxZeroRises={inboxZeroRises}
          isCompact={isCompact}
          onClearFiltersAndContinue={onClearFiltersAndContinue}
          onViewArchived={onViewArchived}
        />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return { ...view, nav, qc, onClearFiltersAndContinue, onViewArchived };
}

beforeEach(() => {
  vi.clearAllMocks();
  spies.guest = false;
  useInboxSelectionStore.setState({
    lastClearedByWorkspace: {},
    leftRowKeyByWorkspace: {},
    sidebarIssueByWorkspace: {},
  });
});
afterEach(() => {
  cleanup();
  takeInboxUndo();
});

describe("InboxEmptyState", () => {
  it("shows calm Inbox Zero with My work, working agents, snoozed summary, and View archived", () => {
    const until = new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString();
    const { nav, onViewArchived } = renderEmpty({
      snoozed: [
        { issue_id: "issue-9", item_id: "n-9", snoozed_until: until, snoozed_at: new Date().toISOString(), updates: 0 },
      ],
      workingAgents: [
        { id: "a-1", name: "Emacs", avatar_url: null, running_task_count: 1, issue_ids: ["issue-1"] },
        { id: "a-2", name: "Codex", avatar_url: null, running_task_count: 1, issue_ids: ["issue-2"] },
      ],
    });

    expect(screen.getByRole("heading", { name: "Inbox cleared" })).toBeInTheDocument();
    expect(screen.getByText(/1 snoozed/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "2 agents working" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "My work" }));
    expect(nav.push).toHaveBeenCalledWith(expect.stringContaining("/acme/issues"));

    fireEvent.click(screen.getByRole("button", { name: "View archived" }));
    expect(onViewArchived).toHaveBeenCalledTimes(1);
  });

  it("offers inline undo for the just-archived notification and navigates back on click", async () => {
    useInboxSelectionStore.setState({
      lastClearedByWorkspace: {
        ws: {
          kind: "archived",
          itemId: "n-1",
          key: "issue-1",
          label: "MUL-9790",
          href: "/acme/issues/MUL-9790",
          clearedAt: Date.now(),
        },
      },
      leftRowKeyByWorkspace: { ws: "issue-1" },
    });

    const { nav } = renderEmpty();
    expect(screen.getByText("Just archived MUL-9790")).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    });
    expect(spies.restoreInbox).toHaveBeenCalledWith("n-1", { quiet: true });
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/MUL-9790");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeUndefined();
    expect(useInboxSelectionStore.getState().sidebarIssueByWorkspace.ws).toBe("issue-1");
  });

  it("opens a restored notification only once it is back in the Inbox list", async () => {
    useInboxSelectionStore.setState({
      lastClearedByWorkspace: {
        ws: {
          kind: "archived",
          itemId: "n-2",
          key: "n-2",
          label: "Automation paused",
          href: "/acme/inbox?issue=n-2",
          clearedAt: Date.now(),
        },
      },
    });
    let finish!: (ok: boolean) => void;
    spies.restoreInbox.mockReturnValueOnce(new Promise<boolean>((resolve) => (finish = resolve)));
    const { nav, qc } = renderEmpty();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(spies.restoreInbox).toHaveBeenCalledTimes(1);
    expect(nav.push).not.toHaveBeenCalled();
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeDefined();

    await act(async () => finish(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxKeys.list("ws") });
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=n-2");
    expect(invalidate.mock.invocationCallOrder[0]!).toBeLessThan(
      (nav.push as ReturnType<typeof vi.fn>).mock.invocationCallOrder[0]!,
    );
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeUndefined();
  });

  it("keeps the undo when it fails, so it can be tried again", async () => {
    useInboxSelectionStore.setState({
      lastClearedByWorkspace: {
        ws: {
          kind: "archived",
          itemId: "n-1",
          key: "issue-1",
          label: "MUL-9790",
          href: "/acme/issues/MUL-9790",
          clearedAt: Date.now(),
        },
      },
    });
    spies.restoreInbox.mockResolvedValueOnce(false);
    const { nav } = renderEmpty();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    });
    expect(nav.push).not.toHaveBeenCalled();
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeDefined();
    expect(screen.getByRole("button", { name: "Undo" })).toBeEnabled();
  });

  it("undoes with Z once the toast is gone, and shows the shortcut beside the undo", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    const { nav } = renderEmpty();
    expect(
      screen.getByRole("button", { name: "Undo" }).querySelector('[data-slot="shortcut-keycaps"]'),
    ).not.toBeNull();

    await act(async () => pressZ());
    expect(spies.restoreInbox).toHaveBeenCalledWith("n-1", { quiet: true });
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/MUL-9790");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeUndefined();
  });

  it("leaves Z to a showing undo toast", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    const { nav } = renderEmpty();
    const toastUndo = vi.fn();
    toastInboxUndo("Notification archived", { label: "Undo", undo: toastUndo });

    await act(async () => pressZ());
    expect(toastUndo).toHaveBeenCalledOnce();
    expect(spies.restoreInbox).not.toHaveBeenCalled();
    expect(nav.push).not.toHaveBeenCalled();
    expect(hasInboxUndo()).toBe(false);
  });

  it("sends one undo when Z is pressed again while it is in flight", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    let finish!: (ok: boolean) => void;
    spies.restoreInbox.mockReturnValueOnce(new Promise<boolean>((resolve) => (finish = resolve)));
    const { nav } = renderEmpty();

    pressZ();
    pressZ();
    expect(spies.restoreInbox).toHaveBeenCalledTimes(1);
    expect(nav.push).not.toHaveBeenCalled();

    await act(async () => finish(true));
    expect(nav.push).toHaveBeenCalledTimes(1);
  });

  it("ignores Z typed into a text field or held down", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    renderEmpty();
    const input = document.createElement("input");
    document.body.appendChild(input);

    await act(async () => {
      pressZ(input);
      fireEvent.keyDown(document.body, { key: "z", code: "KeyZ", repeat: true });
    });
    input.remove();
    expect(spies.restoreInbox).not.toHaveBeenCalled();
  });

  it("undoes with Z from the filtered-empty state", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    const { nav } = renderEmpty({ totalActiveCount: 3, hasActiveFilters: true });
    expect(
      screen.getByRole("button", { name: "Undo MUL-9790" }).querySelector('[data-slot="shortcut-keycaps"]'),
    ).not.toBeNull();

    await act(async () => pressZ());
    expect(spies.restoreInbox).toHaveBeenCalledWith("n-1", { quiet: true });
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/MUL-9790");
  });

  it("does not answer Z in the archived view", async () => {
    useInboxSelectionStore.setState({ lastClearedByWorkspace: { ws: archivedEntry } });
    renderEmpty({ isArchivedView: true });
    await act(async () => pressZ());
    expect(spies.restoreInbox).not.toHaveBeenCalled();
  });

  it("shows filtered-empty state with remaining count and Clear filters CTA instead of Inbox Zero", () => {
    const { container, onClearFiltersAndContinue } = renderEmpty({
      visibleCount: 0,
      totalActiveCount: 4,
      hasActiveFilters: true,
    });

    expect(screen.getByRole("heading", { name: "All caught up for this filter" })).toBeInTheDocument();
    expect(screen.getByText("4 other notifications in your inbox")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Inbox cleared" })).toBeNull();
    // MulticaBleedMark (viewBox 0 0 100 100) must not render in filtered-empty state.
    expect(container.querySelector('svg[viewBox="0 0 100 100"]')).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Clear filters and continue/ }));
    expect(onClearFiltersAndContinue).toHaveBeenCalledTimes(1);
  });

  it("hides member-only shortcuts and working agents for guests", () => {
    spies.guest = true;
    renderEmpty({
      workingAgents: [
        { id: "a-1", name: "Emacs", avatar_url: null, running_task_count: 1, issue_ids: ["issue-1"] },
      ],
    });

    expect(screen.queryByRole("button", { name: "My work" })).toBeNull();
    expect(screen.queryByRole("button", { name: /agent/i })).toBeNull();
    expect(screen.getByRole("button", { name: "View archived" })).toBeInTheDocument();
  });

  it("keeps the archived empty state quiet and separate from Inbox Zero", () => {
    renderEmpty({ isArchivedView: true });
    expect(screen.getByText("No archived notifications")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Inbox cleared" })).toBeNull();
  });
});

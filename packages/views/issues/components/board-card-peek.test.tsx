import { useIssueOpeningStore } from "@multica/core/issues/stores/issue-opening-store";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DndContext } from "@dnd-kit/core";
import { SortableContext } from "@dnd-kit/sortable";
import type { Issue } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import {
  IssuePeekActionsContext,
  IssuePeekIdContext,
  type IssuePeekActions,
} from "../surface/peek-context";
import { DraggableBoardCard } from "./board-card";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({ queryKey: ["properties"] }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({
      viewMode: "board",
      grouping: "status",
      swimlaneGrouping: "assignee",
      cardProperties: {},
      cardPropertyIds: [],
    }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Someone" }),
}));

vi.mock("../../i18n", () => ({
  useLocale: () => "en",
  useT: () => ({ t: () => "Translated" }),
  useTimeAgo: () => () => "now",
}));

vi.mock("../actions", () => ({
  IssueActionsContextMenu: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("./issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));

vi.mock("./custom-status-chip", () => ({
  CustomStatusChip: () => null,
  useIsCustomStatus: () => false,
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const peek: IssuePeekActions = {
  open: vi.fn(),
  toggle: vi.fn(),
  close: vi.fn(),
  publishColumns: vi.fn(),
};

const issue = {
  id: "issue-1",
  identifier: "MUL-1",
  title: "Peekable card",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  labels: [],
  properties: {},
  start_date: null,
  due_date: null,
  updated_at: "2026-09-27T00:00:00Z",
} as unknown as Issue;

function renderCard({ withPeek = true, peekedId = null as string | null, cardIssue = issue } = {}) {
  const card = (
    <DndContext>
      <SortableContext items={[issue.id]}>
        <DraggableBoardCard issue={cardIssue} />
      </SortableContext>
    </DndContext>
  );
  return render(
    <NavigationProvider value={navigation}>
      {withPeek ? (
        <IssuePeekActionsContext.Provider value={peek}>
          <IssuePeekIdContext.Provider value={peekedId}>{card}</IssuePeekIdContext.Provider>
        </IssuePeekActionsContext.Provider>
      ) : (
        card
      )}
    </NavigationProvider>,
  );
}

const link = () => screen.getByRole("link");
const cardRoot = (container: HTMLElement) =>
  container.querySelector<HTMLElement>('[data-peek-target="issue-1"]')!;

describe("DraggableBoardCard side peek", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useIssueOpeningStore.setState({ openMode: "page" });
  });

  it("opens the peek on Shift+Click instead of navigating", () => {
    renderCard();
    expect(fireEvent.click(link(), { shiftKey: true })).toBe(false);
    expect(peek.toggle).toHaveBeenCalledWith("issue-1");
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("still navigates on a plain click", () => {
    renderCard();
    fireEvent.click(link());
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(peek.toggle).not.toHaveBeenCalled();
  });

  it("leaves Cmd/Ctrl+Shift+Click to the new-tab behavior", () => {
    renderCard();
    fireEvent.click(link(), { shiftKey: true, metaKey: true });
    fireEvent.click(link(), { shiftKey: true, ctrlKey: true });
    expect(peek.toggle).not.toHaveBeenCalled();
  });

  it("peeks the focused card on Space", () => {
    const { container } = renderCard();
    expect(fireEvent.keyDown(cardRoot(container), { key: " " })).toBe(false);
    fireEvent.keyDown(link(), { key: " " });
    expect(peek.toggle).toHaveBeenCalledTimes(2);
  });

  it("keeps linked PR keyboard gestures separate from the card's Space peek", () => {
    const { container } = renderCard({
      cardIssue: {
        ...issue,
        linked_pull_requests: [{
          provider: "github",
          number: 7,
          title: "Linked work",
          state: "open",
          html_url: "https://github.com/acme/repo/pull/7",
        }],
      },
    });
    const prLink = screen.getByRole("link", { name: "Translated #7 · Translated" });

    expect(fireEvent.keyDown(prLink, { key: " " })).toBe(true);
    expect(fireEvent.keyDown(prLink, { key: "Enter" })).toBe(true);
    expect(prLink).toHaveAttribute("href", "https://github.com/acme/repo/pull/7");
    expect(prLink).toHaveAttribute("target", "_blank");
    expect(peek.toggle).not.toHaveBeenCalled();
    expect(navigation.push).not.toHaveBeenCalled();

    const issueLink = container.querySelector<HTMLAnchorElement>('a[href="/acme/issues/issue-1"]')!;
    expect(fireEvent.keyDown(issueLink, { key: " " })).toBe(false);
    expect(peek.toggle).toHaveBeenCalledWith("issue-1");
  });

  it("reacts immediately to the preference and keeps repeated plain clicks open", () => {
    renderCard();
    act(() => useIssueOpeningStore.getState().setOpenMode("peek"));
    fireEvent.click(link());
    fireEvent.click(link());
    expect(peek.open).toHaveBeenCalledTimes(2);
    expect(peek.open).toHaveBeenCalledWith("issue-1");
    expect(peek.toggle).not.toHaveBeenCalled();
    expect(navigation.push).not.toHaveBeenCalled();
    act(() => useIssueOpeningStore.getState().setOpenMode("page"));
    fireEvent.click(link());
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-1");
  });

  it("opens the full page in place on Shift+Click when the preference is peek", () => {
    useIssueOpeningStore.getState().setOpenMode("peek");
    renderCard();
    // Handled here, not left to the browser (a new window on web).
    expect(fireEvent.click(link(), { shiftKey: true })).toBe(false);
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(peek.open).not.toHaveBeenCalled();
    expect(peek.toggle).not.toHaveBeenCalled();
  });

  it("still navigates without a peek host when the preference is peek", () => {
    useIssueOpeningStore.getState().setOpenMode("peek");
    renderCard({ withPeek: false });
    fireEvent.click(link());
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(peek.open).not.toHaveBeenCalled();
  });

  it("marks the peeked card", () => {
    const { container } = renderCard({ peekedId: "issue-1" });
    expect(cardRoot(container)).toHaveAttribute("data-peeked");
  });

  it("keeps the browser's shift-click outside a peek host", () => {
    renderCard({ withPeek: false });
    // No handler claims the gesture: AppLink leaves it to the browser.
    expect(fireEvent.click(link(), { shiftKey: true })).toBe(true);
    expect(navigation.push).not.toHaveBeenCalled();
  });
});

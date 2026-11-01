import type { ReactNode } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import type { PinnedItem } from "@multica/core/types";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../../issues/actions";
import { PinnedSection, usePanelPins } from "./pinned-section";

vi.mock("../../issues/actions/issue-actions-menu-items", () => ({
  contextPrimitives: {},
  IssueActionsMenuItems: ({ issue, variant }: { issue: { title: string }; variant?: string }) => (
    <div>{`${variant} menu: ${issue.title}`}</div>
  ),
}));
vi.mock("../../issues/actions/use-issue-actions", () => ({ useIssueActions: () => ({}) }));

const state = vi.hoisted(() => ({
  pins: [] as PinnedItem[],
  detail: { isPending: false, isError: false, data: null as unknown, error: null as unknown },
  deletePin: vi.fn(),
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  useSortable: () => ({ attributes: {}, listeners: {}, setNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
  arrayMove: vi.fn(),
}));
vi.mock("@dnd-kit/utilities", () => ({ CSS: { Translate: { toString: () => undefined } } }));
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: { children: ReactNode; href: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/pins/queries", () => ({ pinListOptions: () => ({ queryKey: ["pins"] }) }));
vi.mock("@multica/core/pins/mutations", () => ({
  useDeletePin: () => ({ mutate: state.deletePin }),
  useReorderPins: () => ({ mutate: vi.fn() }),
}));
vi.mock("@multica/core/issues/queries", () => ({ issueDetailOptions: () => ({ queryKey: ["issue"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectDetailOptions: () => ({ queryKey: ["project"] }) }));
vi.mock("@multica/core/issue-views/queries", () => ({
  issueViewDetailOptions: () => ({ queryKey: ["view"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === "pins") return { data: state.pins };
    if (queryKey[0] === "issue") return state.detail;
    return { isPending: true, isError: false, data: undefined, error: null };
  },
}));

const pin = (n: number): PinnedItem => ({
  id: `pin-${n}`,
  workspace_id: "ws-1",
  user_id: "user-1",
  item_type: "issue",
  item_id: `issue-${n}`,
  position: n,
  created_at: "2026-05-06T00:00:00Z",
});

let activePinnedRoute = false;
function Harness({ pathname, currentIssueId = null }: { pathname: string; currentIssueId?: string | null }) {
  const pins = usePanelPins("ws-1", "user-1", pathname, currentIssueId);
  activePinnedRoute = pins.isActivePinnedRoute;
  return (
    <IssueContextMenuProvider variant="sidebar">
      <PinnedSection pins={pins} wsId="ws-1" />
    </IssueContextMenuProvider>
  );
}

beforeEach(() => {
  state.pins = [pin(1)];
  state.detail = { isPending: false, isError: false, data: null, error: null };
  state.deletePin.mockReset();
  activePinnedRoute = false;
  useSidebarTreeStore.setState({ pinnedCollapsed: false });
});

describe("PinnedSection", () => {
  it("unpins items whose detail is gone", async () => {
    state.detail = { isPending: false, isError: true, data: null, error: new ApiError("missing", 404, "Not Found") };
    renderWithI18n(<Harness pathname="/acme/issues" />);
    await waitFor(() => expect(state.deletePin).toHaveBeenCalledTimes(1));
  });

  it("keeps pins through other errors", async () => {
    state.detail = { isPending: false, isError: true, data: null, error: new ApiError("error", 500, "Server Error") };
    renderWithI18n(<Harness pathname="/acme/issues" />);
    await waitFor(() => expect(state.deletePin).not.toHaveBeenCalled());
  });

  it("renders a loaded issue by its title and marks it active on its page", async () => {
    state.detail = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" },
      error: null,
    };
    // The issue route rewrites the pin's UUID URL to the identifier form.
    renderWithI18n(<Harness pathname="/acme/issues/MUL-123" currentIssueId="issue-1" />);
    const row = (await screen.findByText("Keep this pin")).closest("a");
    expect(row).toHaveAttribute("aria-current", "page");
    expect(screen.queryByText("MUL-123 Keep this pin")).toBeNull();
    expect(activePinnedRoute).toBe(true);
  });

  it("opens the sidebar issue menu when a pinned issue is right-clicked", async () => {
    state.detail = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" },
      error: null,
    };
    renderWithI18n(<Harness pathname="/acme/issues" />);
    const row = (await screen.findByText("Keep this pin")).closest("a")!.parentElement!;
    fireEvent.contextMenu(row);

    expect(await screen.findByText("sidebar menu: Keep this pin")).toBeInTheDocument();
    expect(row).toHaveAttribute("data-popup-open");
  });

  it("previews five pins and only counts a hidden pin as active once expanded", () => {
    state.pins = Array.from({ length: 6 }, (_, i) => pin(i + 1));
    state.detail = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-1", title: "Pinned issue", status: "todo" },
      error: null,
    };
    renderWithI18n(<Harness pathname="/acme/issues/MUL-6" currentIssueId="issue-6" />);
    const lastPin = () => document.querySelector('a[href="/acme/issues/issue-6"]');

    expect(lastPin()).toBeNull();
    expect(activePinnedRoute).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Show 1 more…" }));
    expect(lastPin()).toHaveAttribute("aria-current", "page");
    expect(activePinnedRoute).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Show fewer" }));
    expect(lastPin()).toBeNull();
  });

  it("collapses from the section label and stops claiming the active route", async () => {
    state.detail = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" },
      error: null,
    };
    renderWithI18n(<Harness pathname="/acme/issues/MUL-123" currentIssueId="issue-1" />);
    await screen.findByText("Keep this pin");
    expect(activePinnedRoute).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Pinned" }));
    expect(useSidebarTreeStore.getState().pinnedCollapsed).toBe(true);
    expect(screen.queryByText("Keep this pin")).toBeNull();
    expect(screen.getByRole("button", { name: "Pinned" })).toHaveAttribute("aria-expanded", "false");
    expect(activePinnedRoute).toBe(false);
  });

  it("renders nothing without pins", () => {
    state.pins = [];
    const { container } = renderWithI18n(<Harness pathname="/acme/issues" />);
    expect(container).toBeEmptyDOMElement();
  });
});

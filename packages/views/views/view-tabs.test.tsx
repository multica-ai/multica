import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render as rtlRender, screen, type RenderOptions } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { SavedView, ViewFilters } from "@multica/core/types";
import enCommon from "../locales/en/common.json";

const TEST_RESOURCES = { en: { common: enCommon } };

function makeView(over: Partial<SavedView>): SavedView {
  return {
    id: "v",
    workspace_id: "ws-1",
    creator_id: "user-1",
    name: "View",
    page: "issues",
    project_id: null,
    filters: {},
    display: {},
    position: 0,
    shared: false,
    is_default: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

const h = vi.hoisted(() => ({
  views: [] as unknown[],
  createSpy: { mutateAsync: vi.fn(), isPending: false },
  updateSpy: { mutate: vi.fn(), isPending: false },
  deleteSpy: { mutate: vi.fn(), isPending: false },
  reorderSpy: { mutate: vi.fn(), isPending: false },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    pathname: "/issues",
    searchParams: new URLSearchParams(),
  }),
}));

vi.mock("./use-seed-default-views", () => ({ useSeedDefaultViews: () => {} }));

vi.mock("@multica/core/views", () => ({
  viewListOptions: (wsId: string, page: string, projectId?: string) => ({
    queryKey: ["views", wsId, page, projectId ?? null],
    queryFn: async () => ({ views: h.views }),
    select: (d: { views: unknown[] }) => d.views,
    enabled: true,
  }),
  useCreateView: () => h.createSpy,
  useUpdateView: () => h.updateSpy,
  useDeleteView: () => h.deleteSpy,
  useReorderViews: () => h.reorderSpy,
}));

// Strip Base UI portals (dropdown / alert-dialog / dialog) to pass-through
// wrappers so the management wiring is reachable without driving portals in
// jsdom. The codebase's accepted test approach (see delete-workspace-dialog).
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => render,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogCancel: ({ children }: { children: ReactNode }) => <button type="button">{children}</button>,
  AlertDialogAction: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { ViewTabs } from "./view-tabs";

const DEFAULT_VIEW = makeView({ id: "v-all", name: "All", is_default: true, position: 0 });
const CUSTOM_VIEW = makeView({ id: "v-cust", name: "Urgent", is_default: false, position: 1 });

function renderTabs(props: {
  currentViewId: string | null;
  currentFilters?: ViewFilters;
  onSelectView?: (v: SavedView | null) => void;
}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    );
  }
  const options: RenderOptions = { wrapper: Wrapper };
  return rtlRender(
    <ViewTabs
      page="issues"
      currentViewId={props.currentViewId}
      onSelectView={props.onSelectView ?? vi.fn()}
      resolveDefaultName={(k) => k}
      currentFilters={props.currentFilters}
    />,
    options,
  );
}

describe("ViewTabs management", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    h.views = [DEFAULT_VIEW, CUSTOM_VIEW];
    h.createSpy.isPending = false;
    h.createSpy.mutateAsync.mockResolvedValue(
      makeView({ id: "v-new", name: "New" }),
    );
  });

  it("saves the live filters as a new view via the + button", async () => {
    const user = userEvent.setup();
    const filters: ViewFilters = { statuses: ["todo"] };
    renderTabs({ currentViewId: "v-cust", currentFilters: filters });

    await screen.findByText("Urgent");
    await user.click(screen.getByRole("button", { name: "Save view" }));
    await user.type(screen.getByRole("textbox"), "Backlog grooming");
    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(h.createSpy.mutateAsync).toHaveBeenCalledWith({
      name: "Backlog grooming",
      page: "issues",
      project_id: null,
      filters,
      shared: false,
      // positions are 0 and 1 → next is 2
      position: 2,
    });
  });

  it("renames the active custom view", async () => {
    const user = userEvent.setup();
    renderTabs({ currentViewId: "v-cust", currentFilters: {} });

    await screen.findByText("Urgent");
    await user.click(screen.getByRole("button", { name: "Rename" }));
    const input = screen.getByRole("textbox");
    await user.clear(input);
    await user.type(input, "Critical");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(h.updateSpy.mutate).toHaveBeenCalledWith({ id: "v-cust", name: "Critical" });
  });

  it("toggles sharing on the active custom view", async () => {
    const user = userEvent.setup();
    renderTabs({ currentViewId: "v-cust", currentFilters: {} });

    await screen.findByText("Urgent");
    await user.click(screen.getByRole("button", { name: "Share with workspace" }));

    expect(h.updateSpy.mutate).toHaveBeenCalledWith({ id: "v-cust", shared: true });
  });

  it("deletes the active custom view after confirmation", async () => {
    const user = userEvent.setup();
    renderTabs({ currentViewId: "v-cust", currentFilters: {} });

    await screen.findByText("Urgent");
    await user.click(screen.getByRole("button", { name: "Delete view" }));
    await user.click(screen.getByRole("button", { name: "Delete" }));

    expect(h.deleteSpy.mutate).toHaveBeenCalledWith("v-cust");
  });

  it("hides the management menu for a default view", async () => {
    renderTabs({ currentViewId: "v-all", currentFilters: {} });

    await screen.findByText("All");
    expect(screen.queryByRole("button", { name: "View options" })).toBeNull();
    // + button is still available so users can save the current filters.
    expect(screen.getByRole("button", { name: "Save view" })).toBeInTheDocument();
  });

  it("renders no management affordances when currentFilters is omitted", async () => {
    renderTabs({ currentViewId: "v-cust" });

    await screen.findByText("Urgent");
    expect(screen.queryByRole("button", { name: "Save view" })).toBeNull();
    expect(screen.queryByRole("button", { name: "View options" })).toBeNull();
  });
});

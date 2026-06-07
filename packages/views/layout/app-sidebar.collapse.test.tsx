import { screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SidebarProvider } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppSidebar } from "./app-sidebar";

// Unlike app-sidebar.test.tsx, this suite exercises the REAL sidebar
// primitive so collapse state (data-state, icon-mode tooltips) is genuinely
// rendered. Only the data sources and navigation are mocked.

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSortable: () => ({ attributes: {}, listeners: {}, setNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
  arrayMove: <T,>(arr: T[]) => arr,
}));
vi.mock("@dnd-kit/utilities", () => ({ CSS: { Transform: { toString: () => undefined } } }));

vi.mock("./help-launcher", () => ({ HelpLauncher: () => null }));
vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));
vi.mock("../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../navigation", () => ({
  // Spread the remaining props so Base UI's `useRender` slot can inject the
  // merged button props (aria-label, className, data-*) into the anchor. A
  // mock that drops props would swallow the accessible name the sidebar wires
  // up, defeating the very thing this suite asserts.
  AppLink: ({ children, href, ...rest }: { children: React.ReactNode; href: string }) => (
    <a href={href} {...rest}>{children}</a>
  ),
  useNavigation: () => ({ pathname: "/acme/issues", push: vi.fn() }),
}));
vi.mock("../projects/components/project-icon", () => ({ ProjectIcon: () => <span /> }));
vi.mock("../workspace/workspace-avatar", () => ({ WorkspaceAvatar: () => <span /> }));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  paths: { workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }) },
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    myIssues: () => "/acme/my-issues",
    issues: () => "/acme/issues",
    projects: () => "/acme/projects",
    autopilots: () => "/acme/autopilots",
    agents: () => "/acme/agents",
    squads: () => "/acme/squads",
    usage: () => "/acme/usage",
    runtimes: () => "/acme/runtimes",
    skills: () => "/acme/skills",
    settings: () => "/acme/settings",
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, getBaseUrl: () => "http://127.0.0.1:8080" } };
});
vi.mock("@multica/core/inbox/queries", () => ({ deduplicateInboxItems: (items: unknown[]) => items, inboxKeys: { list: () => ["inbox"] } }));
vi.mock("@multica/core/issues/queries", () => ({ issueDetailOptions: () => ({ queryKey: ["issue"] }) }));
vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: { getState: () => ({ lastMode: "agent" }) },
  openCreateIssueWithPreference: vi.fn(),
}));
vi.mock("@multica/core/issues/stores/draft-store", () => ({ useIssueDraftStore: () => false }));
vi.mock("@multica/core/modals", () => ({ useModalStore: { getState: () => ({ modal: null, open: vi.fn() }) } }));
vi.mock("@multica/core/pins/mutations", () => ({ useDeletePin: () => ({ mutate: vi.fn() }), useReorderPins: () => ({ mutate: vi.fn() }) }));
vi.mock("@multica/core/pins/queries", () => ({ pinListOptions: () => ({ queryKey: ["pins"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectDetailOptions: () => ({ queryKey: ["project"] }) }));
vi.mock("@multica/core/runtimes/hooks", () => ({ useMyRuntimesNeedUpdate: () => false }));
vi.mock("@multica/core/config", () => ({ useConfigStore: () => false }));
vi.mock("@multica/core/workspace/queries", () => ({
  myInvitationListOptions: () => ({ queryKey: ["invitations"] }),
  workspaceKeys: { myInvitations: () => ["invitations"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({ resolvePublicFileUrl: (u?: string) => u }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useMutation: () => ({ isPending: false, mutate: vi.fn() }),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    // No pinned items here — keep the DOM focused on nav rendering.
    if (queryKey[0] === "pins") return { data: [] };
    return { data: [] };
  },
  useQueryClient: () => ({ fetchQuery: vi.fn(), invalidateQueries: vi.fn() }),
}));

function getSidebar(): HTMLElement {
  const el = document.querySelector<HTMLElement>("[data-slot='sidebar']");
  if (!el) throw new Error("sidebar element not found");
  return el;
}

describe("AppSidebar collapse (Phase S1)", () => {
  it("renders collapsed when SidebarProvider defaultOpen is false", () => {
    renderWithI18n(
      <SidebarProvider defaultOpen={false}>
        <AppSidebar />
      </SidebarProvider>,
    );
    expect(getSidebar()).toHaveAttribute("data-state", "collapsed");
  });

  it("renders expanded by default", () => {
    renderWithI18n(
      <SidebarProvider>
        <AppSidebar />
      </SidebarProvider>,
    );
    expect(getSidebar()).toHaveAttribute("data-state", "expanded");
  });
});

describe("AppSidebar icon-mode affordances (Phase S2)", () => {
  it("each top-level nav item exposes an accessible tooltip label when collapsed", () => {
    renderWithI18n(
      <SidebarProvider defaultOpen={false}>
        <AppSidebar />
      </SidebarProvider>,
    );
    // Tooltip content is rendered into a portal only on hover; the contract we
    // assert is that every collapsed nav button carries an accessible name via
    // the wired-up tooltip (aria-label), so screen readers and the icon rail
    // both expose the label even when the text span is visually hidden.
    expect(screen.getByRole("link", { name: "Inbox" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Issues" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings" })).toBeInTheDocument();
  });

  it("toggles open state via the header trigger button", () => {
    renderWithI18n(
      <SidebarProvider defaultOpen={false}>
        <AppSidebar />
      </SidebarProvider>,
    );
    expect(getSidebar()).toHaveAttribute("data-state", "collapsed");
    const trigger = document.querySelector<HTMLElement>("[data-slot='sidebar-trigger']");
    if (!trigger) throw new Error("sidebar trigger not found");
    fireEvent.click(trigger);
    expect(getSidebar()).toHaveAttribute("data-state", "expanded");
  });
});

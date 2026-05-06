/* eslint-disable @typescript-eslint/no-explicit-any */
import {
  cloneElement,
  type ReactElement,
  type ReactNode,
} from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import {
  ProductCapabilitiesProvider,
  type ProductCapabilities,
} from "@multica/core/platform";
import { LOCAL_PRODUCT_CAPABILITIES } from "@multica/core/config";

// ---------------------------------------------------------------------------
// Hoisted mocks / fixtures
// ---------------------------------------------------------------------------

const mockOpenModal = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const mockLogout = vi.hoisted(() => vi.fn());

// Fixture data — accessed from the useQuery mock to vary results by query key.
const fixtures = vi.hoisted(() => ({
  workspaces: [{ id: "w1", slug: "loc", name: "Loc" }],
  myInvitations: [
    {
      id: "inv-1",
      workspace_id: "w-other-1",
      workspace_name: "Pending Workspace 1",
    },
    {
      id: "inv-2",
      workspace_id: "w-other-2",
      workspace_name: "Pending Workspace 2",
    },
  ],
  inbox: [],
  pins: [],
}));

// ---------------------------------------------------------------------------
// React Query — pick fixture by key marker
// ---------------------------------------------------------------------------

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey, enabled }: { queryKey?: unknown[]; enabled?: boolean }) => {
    if (enabled === false) return { data: undefined };
    const marker = Array.isArray(queryKey) ? String(queryKey[0]) : "";
    if (marker === "workspaces:list") return { data: fixtures.workspaces };
    if (marker === "workspaces:myInvitations")
      return { data: fixtures.myInvitations };
    if (marker === "inbox") return { data: fixtures.inbox };
    if (marker === "pins") return { data: fixtures.pins };
    return { data: undefined };
  },
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    fetchQuery: vi.fn().mockResolvedValue([]),
    getQueryData: vi.fn(),
  }),
}));

// ---------------------------------------------------------------------------
// Core stubs
// ---------------------------------------------------------------------------

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: { id: "u1", name: "User", email: "u@u.com" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: { id: "u1", name: "User", email: "u@u.com" } }) },
  ),
}));

vi.mock("@multica/core/paths", () => {
  const navStubs: Record<string, (...args: string[]) => string> = {
    inbox: () => "/loc/inbox",
    myIssues: () => "/loc/my-issues",
    issues: () => "/loc/issues",
    projects: () => "/loc/projects",
    autopilots: () => "/loc/autopilots",
    agents: () => "/loc/agents",
    runtimes: () => "/loc/runtimes",
    skills: () => "/loc/skills",
    settings: () => "/loc/settings",
    issueDetail: (id: string) => `/loc/issues/${id}`,
    projectDetail: (id: string) => `/loc/projects/${id}`,
  };
  return {
    useCurrentWorkspace: () => ({ id: "w1", slug: "loc", name: "Loc" }),
    useWorkspacePaths: () => navStubs,
    paths: {
      workspace: (slug: string) => ({
        issues: () => `/${slug}/issues`,
      }),
    },
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces:list"] }),
  myInvitationListOptions: () => ({ queryKey: ["workspaces:myInvitations"] }),
  workspaceKeys: {
    myInvitations: () => ["workspaces:myInvitations"],
  },
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxKeys: { list: (wsId: string) => ["inbox", wsId] },
  deduplicateInboxItems: (items: unknown[]) => items,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    acceptInvitation: vi.fn(),
    declineInvitation: vi.fn(),
    listInbox: vi.fn().mockResolvedValue([]),
    listWorkspaces: vi.fn().mockResolvedValue([]),
    listMyInvitations: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { open: mockOpenModal, modal: null };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal, modal: null }) },
  ),
}));

vi.mock("@multica/core/runtimes/hooks", () => ({
  useMyRuntimesNeedUpdate: () => false,
}));

vi.mock("@multica/core/pins/queries", () => ({
  pinListOptions: (wsId: string, userId: string) => ({
    queryKey: ["pins", wsId, userId],
  }),
}));

vi.mock("@multica/core/pins/mutations", () => ({
  useDeletePin: () => ({ mutate: vi.fn() }),
  useReorderPins: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: () => ({ queryKey: ["issue-detail"], queryFn: () => null }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectDetailOptions: () => ({ queryKey: ["project-detail"], queryFn: () => null }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: (selector: (s: unknown) => unknown) =>
    selector({ draft: { title: "", description: "" } }),
}));

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: Object.assign(
    () => "manual",
    { getState: () => ({ lastMode: "manual" }) },
  ),
}));

vi.mock("@multica/core/types", () => ({}));

// ---------------------------------------------------------------------------
// Local relative imports
// ---------------------------------------------------------------------------

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: "/", push: mockPush }),
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("../auth", () => ({
  useLogout: () => mockLogout,
}));

vi.mock("../workspace/workspace-avatar", () => ({
  WorkspaceAvatar: ({ name }: { name: string }) => (
    <span data-testid="workspace-avatar">{name}</span>
  ),
}));

vi.mock("../issues/components/status-icon", () => ({
  StatusIcon: () => <span data-testid="status-icon" />,
}));

vi.mock("../projects/components/project-icon", () => ({
  ProjectIcon: () => <span data-testid="project-icon" />,
}));

// ---------------------------------------------------------------------------
// UI primitives — strip portals / open-state to pass-through wrappers
// ---------------------------------------------------------------------------

vi.mock("@multica/ui/components/ui/sidebar", () => {
  const Pass = ({ children }: { children?: ReactNode }) => <div>{children}</div>;
  const SidebarMenuButton = ({
    children,
    render,
    onClick,
  }: {
    children?: ReactNode;
    render?: ReactElement<{ onClick?: () => void }>;
    onClick?: () => void;
  }) => {
    if (render) {
      return children === undefined
        ? cloneElement(render, { onClick })
        : cloneElement(render, { onClick }, children);
    }
    return (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    );
  };
  return {
    Sidebar: Pass,
    SidebarContent: Pass,
    SidebarFooter: Pass,
    SidebarGroup: Pass,
    SidebarGroupContent: Pass,
    SidebarGroupLabel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
    SidebarHeader: Pass,
    SidebarMenu: Pass,
    SidebarMenuButton,
    SidebarMenuItem: Pass,
    SidebarRail: () => null,
  };
});

vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const Pass = ({ children }: { children?: ReactNode }) => <div>{children}</div>;
  const DropdownMenuTrigger = ({
    children,
    render,
  }: {
    children?: ReactNode;
    render?: ReactElement;
  }) => {
    if (render) {
      // When children is undefined, preserve render's own children — passing
      // undefined as the third arg to cloneElement nukes them.
      return children === undefined
        ? cloneElement(render)
        : cloneElement(render, {}, children);
    }
    return <div>{children}</div>;
  };
  const DropdownMenuItem = ({
    children,
    render,
    onClick,
  }: {
    children?: ReactNode;
    render?: ReactElement<{ onClick?: () => void }>;
    onClick?: () => void;
  }) => {
    if (render) {
      return children === undefined
        ? cloneElement(render, { onClick })
        : cloneElement(render, { onClick }, children);
    }
    return (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    );
  };
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger,
    DropdownMenuContent: Pass,
    DropdownMenuGroup: Pass,
    DropdownMenuItem,
    DropdownMenuLabel: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
    DropdownMenuSeparator: () => null,
  };
});

vi.mock("@multica/ui/components/ui/tooltip", () => {
  const Pass = ({ children }: { children?: ReactNode }) => <>{children}</>;
  return {
    Tooltip: Pass,
    TooltipTrigger: Pass,
    TooltipContent: () => null,
  };
});

vi.mock("@multica/ui/components/ui/collapsible", () => {
  const Pass = ({ children }: { children?: ReactNode }) => <div>{children}</div>;
  return { Collapsible: Pass, CollapsibleTrigger: Pass, CollapsibleContent: Pass };
});

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => <span>{name}</span>,
}));

// ---------------------------------------------------------------------------
// dnd-kit — pass-through
// ---------------------------------------------------------------------------

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  PointerSensor: function PointerSensor() {},
  useSensor: () => ({}),
  useSensors: () => [],
  closestCenter: () => null,
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  verticalListSortingStrategy: () => null,
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
  arrayMove: <T,>(arr: T[]) => arr,
}));

vi.mock("@dnd-kit/utilities", () => ({
  CSS: { Transform: { toString: () => "" } },
}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { AppSidebar } from "./app-sidebar";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeCaps(overrides: Partial<ProductCapabilities>): ProductCapabilities {
  return { ...LOCAL_PRODUCT_CAPABILITIES, ...overrides };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("AppSidebar", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("with default (local) capabilities: hides Log out, Pending invitations, and the invitation dot", () => {
    const { container } = render(<AppSidebar />);

    // Log out item is hidden.
    expect(
      screen.queryByRole("button", { name: /log out/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/log out/i)).not.toBeInTheDocument();

    // Pending invitations group is hidden.
    expect(screen.queryByText(/pending invitations/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/pending workspace 1/i)).not.toBeInTheDocument();

    // The brand-colored invitation dot near the workspace avatar is not rendered.
    expect(container.querySelector(".bg-brand")).toBeNull();
  });

  it("with showLogin + showInvitations enabled: renders Log out and Pending invitations", () => {
    const cloudCaps = makeCaps({
      auth: { ...LOCAL_PRODUCT_CAPABILITIES.auth, showLogin: true },
      collaboration: {
        ...LOCAL_PRODUCT_CAPABILITIES.collaboration,
        showInvitations: true,
      },
    });

    const { container } = render(
      <ProductCapabilitiesProvider capabilities={cloudCaps}>
        <AppSidebar />
      </ProductCapabilitiesProvider>,
    );

    expect(screen.getByText(/log out/i)).toBeInTheDocument();
    expect(screen.getByText(/pending invitations/i)).toBeInTheDocument();
    // The workspace name appears at least once in the invitation row.
    expect(screen.getAllByText(/pending workspace 1/i).length).toBeGreaterThan(0);
    expect(container.querySelector(".bg-brand")).not.toBeNull();
  });
});

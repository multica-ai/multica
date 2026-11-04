import type { ReactNode } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { RailWorkspaces } from "./rail-workspaces";

type Ws = { id: string; name: string; slug: string; avatar_url: string | null };

const state = vi.hoisted(() => ({
  workspaces: [] as Ws[],
  invitations: [] as { id: string; workspace_id: string; workspace_name?: string; inviter_name?: string }[],
  summary: [] as { workspace_id: string; count: number }[],
  accept: vi.fn(),
  decline: vi.fn(),
  invalidateQueries: vi.fn(),
  mutations: [] as Array<Record<string, unknown>>,
  workspaceCreationDisabled: false,
  openWorkspaceInNewWindow: undefined as undefined | Mock<(slug: string) => void>,
  workspacesInOtherWindows: undefined as undefined | (() => string[]),
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: { workspaceCreationDisabled: boolean }) => unknown) =>
    selector({ workspaceCreationDisabled: state.workspaceCreationDisabled }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href, ...props }: { children: ReactNode; href: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    pathname: "/active/issues",
    push: vi.fn(),
    openWorkspaceInNewWindow: state.openWorkspaceInNewWindow,
    workspacesInOtherWindows: state.workspacesInOtherWindows,
  }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Active WS", slug: "active" }),
}));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getBaseUrl: () => "http://127.0.0.1:8080",
      acceptInvitation: state.accept,
      declineInvitation: state.decline,
    },
  };
});
vi.mock("@multica/core/inbox/queries", () => ({
  inboxUnreadSummaryOptions: () => ({ queryKey: ["inbox", "unread-summary"] }),
  inboxUnreadSummaryEnabled: () => true,
  unreadWorkspaceIds: (entries: { workspace_id: string; count: number }[]) =>
    new Set(entries.filter((s) => s.count > 0).map((s) => s.workspace_id)),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  myInvitationListOptions: () => ({ queryKey: ["invitations"] }),
  workspaceKeys: { myInvitations: () => ["invitations"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useMutation: (options: Record<string, unknown> = {}) => {
    state.mutations.push(options);
    return { isPending: false, mutate: vi.fn() };
  },
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === "workspaces") return { data: state.workspaces };
    if (queryKey[0] === "invitations") return { data: state.invitations };
    if (queryKey[0] === "inbox") return { data: state.summary };
    return { data: undefined };
  },
  useQueryClient: () => ({ fetchQuery: vi.fn(), invalidateQueries: state.invalidateQueries }),
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

const ws = (n: number): Ws => ({ id: `ws-${n}`, name: n === 1 ? "Active WS" : `WS ${n}`, slug: n === 1 ? "active" : `ws${n}`, avatar_url: null });
const trigger = () => screen.getByRole("button", { name: "Switch workspace" });
const triggerHasDot = () => !!trigger().querySelector(".bg-brand");
const openMenu = async () => {
  fireEvent.click(trigger());
  return screen.findByRole("menu");
};

beforeEach(() => {
  state.workspaces = [ws(1), ws(2)];
  state.invitations = [];
  state.summary = [];
  state.mutations.length = 0;
  state.workspaceCreationDisabled = false;
  state.openWorkspaceInNewWindow = undefined;
  state.workspacesInOtherWindows = undefined;
});

describe("RailWorkspaces", () => {
  it("shows only the current workspace on the rail, however many there are", async () => {
    state.workspaces = [ws(2), ws(3), ws(4), ws(5), ws(6), ws(1)];
    renderWithI18n(<RailWorkspaces />);
    expect(screen.queryAllByRole("link")).toHaveLength(0);

    const menu = await openMenu();
    const items = within(menu).getAllByRole("menuitem").filter((el) => el.getAttribute("href") !== "/workspaces/new");
    expect(items.map((el) => el.textContent?.slice(1))).toEqual(["WS 2", "WS 3", "WS 4", "WS 5", "WS 6", "Active WS"]);
    expect(within(menu).getByRole("menuitem", { name: /Active WS$/ })).toHaveAttribute("aria-current", "page");
    expect(within(menu).getByRole("menuitem", { name: /WS 3$/ })).toHaveAttribute("href", "/ws3/issues");
  });

  it("dots the switcher while another workspace has unread inbox items", async () => {
    state.summary = [{ workspace_id: "ws-2", count: 3 }];
    renderWithI18n(<RailWorkspaces />);
    expect(triggerHasDot()).toBe(true);
    const menu = await openMenu();
    expect(within(menu).getByRole("menuitem", { name: /WS 2$/ }).querySelector(".bg-brand")).not.toBeNull();
  });

  // The current workspace's unread is the Inbox space's count.
  it("does not dot the switcher for the current workspace or zero counts", () => {
    state.summary = [
      { workspace_id: "ws-1", count: 5 },
      { workspace_id: "ws-2", count: 0 },
    ];
    renderWithI18n(<RailWorkspaces />);
    expect(triggerHasDot()).toBe(false);
  });

  it("lists pending invitations in the menu and dots the switcher", async () => {
    state.invitations = [{ id: "inv-1", workspace_id: "ws-9", workspace_name: "Copilot Hub" }];
    renderWithI18n(<RailWorkspaces />);
    expect(triggerHasDot()).toBe(true);
    const menu = await openMenu();
    expect(within(menu).getByText("Pending invitations")).toBeInTheDocument();
    expect(within(menu).getByRole("menuitem", { name: /Copilot Hub$/ })).toBeInTheDocument();
  });

  it("offers workspace creation unless the instance disables it", async () => {
    const { unmount } = renderWithI18n(<RailWorkspaces />);
    expect(within(await openMenu()).getByRole("menuitem", { name: "Create workspace" })).toBeInTheDocument();
    unmount();

    state.workspaceCreationDisabled = true;
    renderWithI18n(<RailWorkspaces />);
    expect(within(await openMenu()).queryByRole("menuitem", { name: "Create workspace" })).toBeNull();
  });

  describe("new windows", () => {
    const row = (menu: HTMLElement, name: RegExp) => within(menu).getByRole("menuitem", { name });

    beforeEach(() => {
      state.openWorkspaceInNewWindow = vi.fn();
      state.workspaces = [ws(1), ws(2), ws(3)];
    });

    it("opens a workspace in a new window from its row button without following the row link", async () => {
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();
      const button = within(row(menu, /WS 3/)).getByRole("button", { name: "Open in new window" });
      const notPrevented = fireEvent.click(button);

      expect(state.openWorkspaceInNewWindow).toHaveBeenCalledWith("ws3");
      expect(notPrevented).toBe(false);
    });

    it("opens a workspace in a new window on shift-click", async () => {
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();
      const target = row(menu, /WS 2/);
      const notPrevented = fireEvent.click(target, { shiftKey: true });

      expect(state.openWorkspaceInNewWindow).toHaveBeenCalledWith("ws2");
      expect(notPrevented).toBe(false);
    });

    it("leaves a plain or cmd+shift click to the row link", async () => {
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();
      fireEvent.click(row(menu, /WS 2/));
      fireEvent.click(row(menu, /WS 2/), { shiftKey: true, metaKey: true });
      expect(state.openWorkspaceInNewWindow).not.toHaveBeenCalled();
    });

    it("marks a workspace another window shows instead of offering a new window for it", async () => {
      state.workspacesInOtherWindows = () => ["ws2"];
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();

      const shown = row(menu, /WS 2/);
      expect(within(shown).getByText("Other window")).toBeInTheDocument();
      expect(within(row(menu, /WS 3/)).queryByText("Other window")).toBeNull();
      expect(within(shown).queryByRole("button")).toBeNull();
      expect(within(row(menu, /WS 3/)).getByRole("button", { name: "Open in new window" })).toBeInTheDocument();
      fireEvent.click(shown, { shiftKey: true });
      expect(state.openWorkspaceInNewWindow).not.toHaveBeenCalled();
    });

    it("offers no new window for the current workspace", async () => {
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();
      expect(within(row(menu, /Active WS/)).queryByRole("button")).toBeNull();
    });

    it("offers no new windows on web", async () => {
      state.openWorkspaceInNewWindow = undefined;
      renderWithI18n(<RailWorkspaces />);
      const menu = await openMenu();
      expect(within(menu).queryByRole("button", { name: "Open in new window" })).toBeNull();
    });
  });
});

describe("pending invitation self-heal", () => {
  beforeEach(() => {
    state.accept.mockReset().mockRejectedValue(new Error("invitation is not pending"));
    state.decline.mockReset().mockRejectedValue(new Error("invitation is not pending"));
    state.invalidateQueries.mockClear();
  });

  // "Invitation is not pending": it was concluded on another surface. Both
  // mutations must refetch the list on failure so the stale tile drops.
  it("invalidates the pending list when accept or decline fails", async () => {
    renderWithI18n(<RailWorkspaces />);
    expect(state.mutations).toHaveLength(2);

    for (const options of state.mutations as Array<{
      mutationFn: (id: string) => Promise<unknown>;
      onError?: (...args: unknown[]) => unknown;
      onSettled?: (...args: unknown[]) => unknown;
    }>) {
      const settle = options.onError ?? options.onSettled;
      expect(settle).toBeTypeOf("function");
      state.invalidateQueries.mockClear();
      await expect(options.mutationFn("inv-1")).rejects.toThrow("invitation is not pending");
      await settle!(new Error("invitation is not pending"), "inv-1", undefined, undefined);
      expect(state.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["invitations"] });
    }
    expect(state.accept).toHaveBeenCalledTimes(1);
    expect(state.decline).toHaveBeenCalledTimes(1);
  });
});

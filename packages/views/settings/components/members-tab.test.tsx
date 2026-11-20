import { useSyncExternalStore, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { configStore } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { renderWithI18n } from "../../test/i18n";

const mockCreateMember = vi.hoisted(() => vi.fn());
const mockRevokeInvitation = vi.hoisted(() => vi.fn());
// The URL the settings router holds; `setSearch` stands in for navigation.
const url = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  const state = { search: "" };
  return {
    get: () => state.search,
    set: (search: string) => {
      state.search = search;
      listeners.forEach((listener) => listener());
    },
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
});
const data = vi.hoisted(() => ({
  members: [] as Array<Record<string, unknown>>,
  invitations: [] as Array<Record<string, unknown>>,
  shareLinks: [] as Array<Record<string, unknown>>,
  summary: null as Record<string, unknown> | null,
  memberLimit: null as Record<string, unknown> | null,
  invitationsLoaded: true,
}));
const mockInvalidateQueries = vi.hoisted(() => vi.fn(() => Promise.resolve()));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(opts: T) => opts,
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes('"limit"')) return { data: data.memberLimit };
    if (key.includes("invitations")) {
      return data.invitationsLoaded
        ? { data: data.invitations, isSuccess: true }
        : { data: undefined, isSuccess: false };
    }
    if (key.includes("share-links")) return { data: data.shareLinks };
    if (key.includes("members")) return { data: data.members, isSuccess: true };
    if (key.includes("projects")) return { data: [] };
    return { data: data.summary };
  },
  useQueryClient: () => ({
    invalidateQueries: mockInvalidateQueries,
    fetchQuery: vi.fn(),
  }),
}));
vi.mock("@multica/core/billing", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/billing")>()),
  usePreviewWorkspaceSeatPurchase: () => ({ mutateAsync: vi.fn() }),
  usePurchaseWorkspaceSeats: () => ({ mutateAsync: vi.fn() }),
  workspaceSubscriptionSummaryOptions: (wsId: string) => ({
    queryKey: ["billing", wsId, "summary"],
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { createMember: mockCreateMember, revokeInvitation: mockRevokeInvitation },
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  const useAuthStore = Object.assign(
    (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
    { getState: () => state },
  );
  return { useAuthStore };
});
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children: ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
  useOptionalNavigation: () => {
    const search = useSyncExternalStore(url.subscribe, url.get);
    const go = (href: string) => url.set(href.split("?")[1] ?? "");
    return {
      pathname: "/acme/settings",
      searchParams: new URLSearchParams(search),
      push: go,
      replace: go,
    };
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { readUpgradeInvite, saveUpgradeInvite } from "@multica/core/billing";
import { MembersTab } from "./members-tab";

const member = (id: string, name: string, role: string) => ({
  id: `m-${id}`,
  user_id: id,
  workspace_id: "ws-1",
  role,
  name,
  email: `${name.toLowerCase().split(" ")[0]}@acme.dev`,
  avatar_url: null,
  created_at: "2025-01-08T00:00:00Z",
});

beforeEach(() => {
  vi.clearAllMocks();
  // clearAllMocks keeps implementations; a rejection set by one test must not
  // leak into the next.
  mockCreateMember.mockReset();
  mockRevokeInvitation.mockReset();
  url.set("tab=members");
  data.members = [
    member("user-1", "Ada Lovelace", "owner"),
    member("user-2", "Grace Hopper", "admin"),
    member("user-3", "Alan Turing", "member"),
  ];
  data.invitations = [
    {
      id: "inv-1",
      workspace_id: "ws-1",
      inviter_id: "user-1",
      invitee_email: "linus@acme.dev",
      invitee_user_id: null,
      role: "member",
      status: "pending",
      created_at: "2025-03-01T00:00:00Z",
      updated_at: "2025-03-01T00:00:00Z",
      expires_at: "2025-03-08T00:00:00Z",
    },
  ];
  data.shareLinks = [];
  data.summary = null;
  data.memberLimit = null;
  data.invitationsLoaded = true;
  localStorage.clear();
  configStore.getState().setFeatureFlags({});
});

describe("MembersTab", () => {
  it("lists members with their role and marks the current user", () => {
    renderWithI18n(<MembersTab />);

    expect(screen.getByRole("tab", { name: /Members\s*3/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("You")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for Grace Hopper" })).toBeInTheDocument();
    // Nobody manages their own membership from this list.
    expect(screen.queryByRole("button", { name: "Actions for Ada Lovelace" })).toBeNull();
  });

  it("filters members by name or email", () => {
    renderWithI18n(<MembersTab />);
    const search = screen.getByRole("searchbox", { name: "Search name or email" });

    fireEvent.change(search, { target: { value: "grace@" } });
    expect(screen.getByText("Grace Hopper")).toBeInTheDocument();
    expect(screen.queryByText("Alan Turing")).toBeNull();

    fireEvent.change(search, { target: { value: "nobody" } });
    expect(screen.getByText("No members match this search.")).toBeInTheDocument();
  });

  it("shows when each pending invitation was sent and expires", async () => {
    const user = userEvent.setup();
    renderWithI18n(<MembersTab />);

    await user.click(screen.getByRole("tab", { name: /Pending invitations\s*1/ }));
    expect(screen.getByText("linus@acme.dev")).toBeInTheDocument();
    expect(screen.getByText(/Sent .* · Expires /)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Revoke invitation for linus@acme.dev" }),
    ).toBeInTheDocument();
  });

  it("keeps the open list in the URL and follows it while on the page", async () => {
    const user = userEvent.setup();
    renderWithI18n(<MembersTab />);

    await user.click(screen.getByRole("tab", { name: /Pending invitations\s*1/ }));
    expect(url.get()).toBe("tab=members&section=invitations");

    // A search result or back/forward changes the URL without remounting.
    act(() => url.set("tab=members&section=links"));
    expect(screen.getByRole("tab", { name: /Share links\s*0/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    act(() => url.set("tab=members"));
    expect(screen.getByRole("tab", { name: /Members\s*3/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("opens the tab a deep link names", () => {
    url.set("tab=members&section=links");
    renderWithI18n(<MembersTab />);
    expect(screen.getByRole("tab", { name: /Share links\s*0/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("No share links yet.")).toBeInTheDocument();
  });

  it("invites from a dialog and closes it once the invitation is sent", async () => {
    mockCreateMember.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithI18n(<MembersTab />);

    await user.click(screen.getByRole("button", { name: "Invite member" }));
    const dialog = await screen.findByRole("dialog", { name: "Invite member" });
    await user.type(within(dialog).getByRole("textbox", { name: "Email" }), "new@acme.dev");
    await user.click(within(dialog).getByRole("button", { name: "Invite" }));

    await waitFor(() =>
      expect(mockCreateMember).toHaveBeenCalledWith("ws-1", {
        email: "new@acme.dev",
        role: "member",
      }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("keeps the invite dialog open when sending fails", async () => {
    mockCreateMember.mockRejectedValue(new Error("nope"));
    const user = userEvent.setup();
    renderWithI18n(<MembersTab />);

    await user.click(screen.getByRole("button", { name: "Invite member" }));
    const dialog = await screen.findByRole("dialog", { name: "Invite member" });
    await user.type(within(dialog).getByRole("textbox", { name: "Email" }), "new@acme.dev");
    await user.click(within(dialog).getByRole("button", { name: "Invite" }));

    await waitFor(() => expect(mockCreateMember).toHaveBeenCalled());
    expect(screen.getByRole("dialog", { name: "Invite member" })).toBeInTheDocument();
  });

  it("links seat usage to billing when subscriptions are on", () => {
    configStore
      .getState()
      .setFeatureFlags({ [BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG]: true });
    data.summary = { seatCapacity: { used: 3, reserved: 1, purchased: 5 } };
    renderWithI18n(<MembersTab />);

    expect(screen.getByText(/4 of 5 seats used/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Manage seats" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=billing",
    );
  });

  describe("member limit", () => {
    const LIMIT_KEY = ["workspaces", "ws-1", "members", "limit"];
    const enableBilling = () =>
      configStore.getState().setFeatureFlags({ [BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG]: true });
    const openInvite = async (user: ReturnType<typeof userEvent.setup>, email = "sarah@acme.dev") => {
      await user.click(screen.getByRole("button", { name: "Invite member" }));
      const dialog = await screen.findByRole("dialog", { name: "Invite member" });
      await user.type(within(dialog).getByRole("textbox", { name: "Email" }), email);
      return dialog;
    };

    it("turns a full enforced limit into upgrade and invite, holding the invitation for that checkout", async () => {
      enableBilling();
      data.memberLimit = { action: "enforce", limit: 5, members: 4, pendingInvitations: 1 };
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user);
      expect(within(dialog).getByRole("alert")).toHaveTextContent(
        "This plan allows up to 5 members. The workspace is full.",
      );
      expect(within(dialog).getByRole("alert")).toHaveTextContent("Pending invitations take a slot too (1 now)");
      expect(within(dialog).getByText("5 / 5 members")).toBeInTheDocument();
      expect(within(dialog).queryByRole("button", { name: "Invite" })).toBeNull();

      await user.click(within(dialog).getByRole("button", { name: "Upgrade and invite" }));

      expect(mockCreateMember).not.toHaveBeenCalled();
      const params = new URLSearchParams(url.get());
      expect(params.get("tab")).toBe("billing");
      expect(params.get("upgrade")).toBe("pro");
      const token = params.get("held_invite");
      expect(token).toBeTruthy();
      expect(readUpgradeInvite("acme", token!)).toMatchObject({ email: "sarah@acme.dev", role: "member", checkout: null });
    });

    it.each([
      ["not-an-email", "Enter a valid email address."],
      ["ada@acme.dev", "ada@acme.dev is already a member of this workspace."],
      ["LINUS@acme.dev", "An invitation to LINUS@acme.dev is already pending."],
    ])("does not start an upgrade for %s, which could not be invited after paying", async (email, message) => {
      enableBilling();
      data.memberLimit = { action: "enforce", limit: 5, members: 4, pendingInvitations: 1 };
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user, email);
      await user.click(within(dialog).getByRole("button", { name: "Upgrade and invite" }));

      expect(toast.error).toHaveBeenCalledWith(message);
      expect(new URLSearchParams(url.get()).get("tab")).toBe("members");
      expect(localStorage.getItem("multica:upgrade_invite:acme")).toBeNull();
      expect(screen.getByRole("dialog", { name: "Invite member" })).toBeInTheDocument();
    });

    it("waits for the invitation list before an upgrade can buy a seat", async () => {
      enableBilling();
      data.memberLimit = { action: "enforce", limit: 5, members: 4, pendingInvitations: 1 };
      data.invitationsLoaded = false;
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user, "linus@acme.dev");
      const upgrade = within(dialog).getByRole("button", { name: "Upgrade and invite" });
      expect(upgrade).toBeDisabled();
      await user.click(upgrade);
      await user.keyboard("{Enter}");

      expect(new URLSearchParams(url.get()).get("tab")).toBe("members");
      expect(localStorage.getItem("multica:upgrade_invite:acme")).toBeNull();
    });

    it("offers no upgrade when Billing is off, only ways to free a slot", async () => {
      data.memberLimit = { action: "enforce", limit: 5, members: 5, pendingInvitations: 0 };
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user);
      expect(within(dialog).getByRole("alert")).toHaveTextContent(
        "Revoke a pending invitation or remove a member to free a slot.",
      );
      expect(within(dialog).queryByRole("button", { name: "Upgrade and invite" })).toBeNull();
      expect(within(dialog).getByRole("button", { name: "Invite" })).toBeDisabled();
    });

    it("stays out of the way while the limit is only observed", async () => {
      enableBilling();
      data.memberLimit = { action: "observe", limit: 5, members: 6, pendingInvitations: 0 };
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user);
      expect(within(dialog).queryByRole("alert")).toBeNull();
      expect(within(dialog).queryByText(/members$/)).toBeNull();
      await user.click(within(dialog).getByRole("button", { name: "Invite" }));
      await waitFor(() => expect(mockCreateMember).toHaveBeenCalled());
    });

    it("sends the held invitation Billing hands back and consumes only that one", async () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "admin" });
      const other = saveUpgradeInvite("acme", { email: "omar@acme.dev", role: "member" });
      url.set(`tab=members&invite=resume&held_invite=${token}`);
      renderWithI18n(<MembersTab />);

      await waitFor(() =>
        expect(mockCreateMember).toHaveBeenCalledWith("ws-1", {
          email: "sarah@acme.dev",
          role: "admin",
        }),
      );
      await waitFor(() => expect(readUpgradeInvite("acme", token)).toBeNull());
      expect(mockCreateMember).toHaveBeenCalledTimes(1);
      // Another tab's held invitation is still waiting on its own checkout.
      expect(readUpgradeInvite("acme", other)).not.toBeNull();
      const params = new URLSearchParams(url.get());
      expect(params.has("invite")).toBe(false);
      expect(params.has("held_invite")).toBe(false);
    });

    // Right after the upgrade the server may still refuse on the Free policy
    // it re-read moments before payment; the send retries past that window.
    it("retries a held invitation the stale Free policy refused, then consumes it", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        mockCreateMember
          .mockRejectedValueOnce(new ApiError("full", 403, "Forbidden", { code: "member_limit_reached" }))
          .mockResolvedValueOnce(undefined);
        const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
        url.set(`tab=members&invite=resume&held_invite=${token}`);
        renderWithI18n(<MembersTab />);

        await waitFor(() => expect(mockCreateMember).toHaveBeenCalledTimes(1));
        expect(readUpgradeInvite("acme", token)).not.toBeNull();
        await vi.advanceTimersByTimeAsync(6_000);
        await waitFor(() => expect(mockCreateMember).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(readUpgradeInvite("acme", token)).toBeNull());
        expect(toast.error).not.toHaveBeenCalled();
      } finally {
        vi.useRealTimers();
      }
    });

    it("keeps a held invitation that still cannot be sent, prefilled for another try", async () => {
      mockCreateMember.mockRejectedValue(new Error("nope"));
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
      url.set(`tab=members&invite=resume&held_invite=${token}`);
      renderWithI18n(<MembersTab />);

      await waitFor(() => expect(mockCreateMember).toHaveBeenCalled());
      const dialog = screen.getByRole("dialog", { name: "Invite member" });
      expect(within(dialog).getByRole("textbox", { name: "Email" })).toHaveValue("sarah@acme.dev");
      expect(readUpgradeInvite("acme", token)).not.toBeNull();
    });

    it("sends a full workspace with a lapsed subscription to Billing, holding nothing", async () => {
      enableBilling();
      data.summary = { availableActions: { checkout: false } };
      data.memberLimit = { action: "enforce", limit: 5, members: 5, pendingInvitations: 0 };
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user);
      expect(within(dialog).getByRole("alert")).toHaveTextContent(
        "The workspace's subscription needs attention. Restore it in Billing to add members",
      );
      expect(within(dialog).queryByRole("button", { name: "Upgrade and invite" })).toBeNull();
      await user.click(within(dialog).getByRole("button", { name: "Open Billing" }));

      const params = new URLSearchParams(url.get());
      expect(params.get("tab")).toBe("billing");
      expect(params.has("upgrade")).toBe(false);
      expect(localStorage.getItem("multica:upgrade_invite:acme")).toBeNull();
    });

    it("refreshes the limit once a revoked invitation frees its slot", async () => {
      mockRevokeInvitation.mockResolvedValue(undefined);
      url.set("tab=members&section=invitations");
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      await user.click(screen.getByRole("button", { name: "Revoke invitation for linus@acme.dev" }));
      await user.click(await screen.findByRole("button", { name: "Confirm" }));

      await waitFor(() => expect(mockRevokeInvitation).toHaveBeenCalledWith("ws-1", "inv-1"));
      await waitFor(() => expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: LIMIT_KEY }));
    });

    it("refetches the limit when the server refuses a member past it", async () => {
      mockCreateMember.mockRejectedValue(
        new ApiError("full", 403, "Forbidden", { code: "member_limit_reached" }),
      );
      const user = userEvent.setup();
      renderWithI18n(<MembersTab />);

      const dialog = await openInvite(user);
      await user.click(within(dialog).getByRole("button", { name: "Invite" }));

      await waitFor(() =>
        expect(toast.error).toHaveBeenCalledWith(
          "The workspace is full. Upgrade to invite more members.",
        ),
      );
      expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: LIMIT_KEY });
      expect(screen.getByRole("dialog", { name: "Invite member" })).toBeInTheDocument();
    });
  });

  it("gives members a read-only view without invite or share-link controls", () => {
    data.members = [
      member("user-1", "Ada Lovelace", "member"),
      member("user-2", "Grace Hopper", "owner"),
    ];
    renderWithI18n(<MembersTab />);

    expect(screen.getByRole("note")).toHaveTextContent("Ask Grace Hopper");
    expect(screen.queryByRole("button", { name: "Invite member" })).toBeNull();
    expect(screen.queryByRole("tab", { name: /Share links/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Actions for/ })).toBeNull();
  });
});

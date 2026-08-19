import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  AuthorityClientError,
  type AuthorityWorkspace,
  type TagAuthorityClient,
} from "@multica/core/tag-authority";
import {
  AuthorityCreateWorkspacePage,
  AuthorityInvitePage,
  AuthorityJoinPage,
  AuthorityMembersPage,
} from "./tag-authority-pages";
import enTagAuthority from "../locales/en/tag-authority.json";

const TEST_RESOURCES = { en: { "tag-authority": enTagAuthority } };

const ownerWorkspace: AuthorityWorkspace = {
  id: "workspace-1",
  slug: "alpha",
  name: "Alpha",
  role: "owner",
  authorityVersion: 4,
  membershipGeneration: 1,
  capabilities: {
    collaborate: true,
    viewMembers: true,
    leaveWorkspace: true,
    manageWorkspace: true,
    manageOwners: true,
    manageAdmins: true,
    manageMembers: true,
    inviteMembers: true,
  },
};

function client(overrides: Partial<TagAuthorityClient>): TagAuthorityClient {
  return {
    listWorkspaces: vi.fn(async () => [ownerWorkspace]),
    createWorkspace: vi.fn(),
    switchWorkspace: vi.fn(),
    listMembers: vi.fn(async () => []),
    changeMember: vi.fn(),
    listInvitations: vi.fn(async () => []),
    issueInvitation: vi.fn(),
    actOnInvitation: vi.fn(),
    acceptInvitation: vi.fn(),
    declineInvitation: vi.fn(),
    createJoinLink: vi.fn(),
    revokeJoinLink: vi.fn(),
    claimJoinLink: vi.fn(),
    exchangeHandoff: vi.fn(),
    ...overrides,
  } as TagAuthorityClient;
}

function wrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      </I18nProvider>
    );
  };
}

describe("AuthorityCreateWorkspacePage", () => {
  it("keeps the mature name/slug form in provisioning until projection is ready", async () => {
    const user = userEvent.setup();
    let finishCreate!: (value: {
      ok: true;
      workspaceId: string;
      authorityVersion: number;
      membershipGeneration: number;
      projectionReady: false;
    }) => void;
    const createWorkspace = vi.fn(
      () =>
        new Promise<{
          ok: true;
          workspaceId: string;
          authorityVersion: number;
          membershipGeneration: number;
          projectionReady: false;
        }>((resolve) => {
          finishCreate = resolve;
        }),
    );
    const onReady = vi.fn();

    render(
      <AuthorityCreateWorkspacePage
        client={client({ createWorkspace })}
        onReady={onReady}
      />,
      { wrapper: wrapper() },
    );

    await user.type(screen.getByLabelText("Workspace name"), "Design Team");
    expect(screen.getByLabelText("Workspace URL")).toHaveValue("design-team");
    await user.click(screen.getByRole("button", { name: "Create Workspace" }));
    expect(screen.getByRole("status")).toHaveTextContent(
      "Provisioning Workspace",
    );

    finishCreate({
      ok: true,
      workspaceId: "workspace-1",
      authorityVersion: 1,
      membershipGeneration: 1,
      projectionReady: false,
    });
    await waitFor(() => expect(onReady).toHaveBeenCalledWith(ownerWorkspace));
  });
});

describe("AuthorityMembersPage", () => {
  it("does not request or reveal pending invitation PII to an ordinary Member", async () => {
    const listInvitations = vi.fn(async () => [
      {
        id: "invite-1",
        targetEmail: "private@example.com",
        role: "member" as const,
        status: "pending" as const,
        expiresAt: new Date("2026-08-26T00:00:00Z"),
      },
    ]);
    const memberWorkspace: AuthorityWorkspace = {
      ...ownerWorkspace,
      role: "member",
      capabilities: {
        ...ownerWorkspace.capabilities,
        manageWorkspace: false,
        manageOwners: false,
        manageAdmins: false,
        manageMembers: false,
        inviteMembers: false,
      },
    };

    render(
      <AuthorityMembersPage
        workspace={memberWorkspace}
        currentUserId="user-1"
        buildJoinLinkUrl={(token) =>
          `https://vibes.test/tag/join?token=${token}`
        }
        client={client({
          listMembers: vi.fn(async () => [
            {
              userId: "user-1",
              name: "Jiachi",
              role: "member" as const,
              membershipGeneration: 1,
            },
          ]),
          listInvitations,
        })}
      />,
      { wrapper: wrapper() },
    );

    expect(await screen.findByText("Jiachi")).toBeVisible();
    expect(listInvitations).not.toHaveBeenCalled();
    expect(screen.queryByText("private@example.com")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Invite Member" }),
    ).not.toBeInTheDocument();
  });

  it("does not let an Admin operate on pending Admin invitations", async () => {
    const adminWorkspace: AuthorityWorkspace = {
      ...ownerWorkspace,
      role: "admin",
      capabilities: {
        ...ownerWorkspace.capabilities,
        manageWorkspace: false,
        manageOwners: false,
        manageAdmins: false,
        manageMembers: true,
      },
    };

    render(
      <AuthorityMembersPage
        workspace={adminWorkspace}
        currentUserId="admin-1"
        buildJoinLinkUrl={(token) =>
          `https://vibes.test/tag/join?token=${token}`
        }
        client={client({
          listInvitations: vi.fn(async () => [
            {
              id: "invite-admin",
              targetEmail: "admin@example.com",
              role: "admin" as const,
              status: "pending" as const,
              expiresAt: new Date("2026-08-26T00:00:00Z"),
            },
            {
              id: "invite-member",
              targetEmail: "member@example.com",
              role: "member" as const,
              status: "pending" as const,
              expiresAt: new Date("2026-08-26T00:00:00Z"),
            },
          ]),
        })}
      />,
      { wrapper: wrapper() },
    );

    const adminInvite = await screen.findByRole("group", {
      name: "admin@example.com",
    });
    expect(within(adminInvite).queryByRole("button")).not.toBeInTheDocument();
    const memberInvite = screen.getByRole("group", {
      name: "member@example.com",
    });
    expect(
      within(memberInvite).getByRole("button", { name: "Resend invitation" }),
    ).toBeVisible();
    expect(
      within(memberInvite).getByRole("button", { name: "Revoke invitation" }),
    ).toBeVisible();
  });

  it("exposes invite, resend, role revision, revoke, and Member-only Join Link controls to an Owner", async () => {
    const user = userEvent.setup();
    const issueInvitation = vi.fn(async () => ({ ok: true as const }));
    const actOnInvitation = vi.fn(async () => ({ ok: true as const }));
    const createJoinLink = vi.fn(async () => ({
      ok: true as const,
      token: "join-token",
      joinLink: {
        id: "link-1",
        offeredRole: "member" as const,
        status: "active" as const,
        maxClaims: 10,
        claimCount: 0,
        expiresAt: new Date("2026-08-26T00:00:00Z"),
      },
    }));
    const revokeJoinLink = vi.fn(async () => ({
      ok: true as const,
      status: "revoked" as const,
    }));

    render(
      <AuthorityMembersPage
        workspace={ownerWorkspace}
        currentUserId="owner-1"
        buildJoinLinkUrl={(token) =>
          `https://vibes.test/tag/join?token=${token}`
        }
        client={client({
          listMembers: vi.fn(async () => [
            {
              userId: "owner-1",
              name: "Owner",
              role: "owner" as const,
              membershipGeneration: 1,
            },
          ]),
          listInvitations: vi.fn(async () => [
            {
              id: "invite-1",
              targetEmail: "member@example.com",
              role: "member" as const,
              status: "pending" as const,
              expiresAt: new Date("2026-08-26T00:00:00Z"),
            },
          ]),
          issueInvitation,
          actOnInvitation,
          createJoinLink,
          revokeJoinLink,
        })}
      />,
      { wrapper: wrapper() },
    );

    await screen.findByText("member@example.com");
    await user.click(screen.getByRole("button", { name: "Resend invitation" }));
    expect(actOnInvitation).toHaveBeenCalledWith("invite-1", "resend");
    await user.click(
      screen.getByRole("button", { name: "Change role to admin" }),
    );
    expect(issueInvitation).toHaveBeenCalledWith("workspace-1", {
      targetEmail: "member@example.com",
      role: "admin",
    });
    await user.click(screen.getByRole("button", { name: "Revoke invitation" }));
    expect(actOnInvitation).toHaveBeenCalledWith("invite-1", "revoke");

    await user.click(screen.getByRole("button", { name: "Create Join Link" }));
    expect(createJoinLink).toHaveBeenCalledWith("workspace-1", {
      maxClaims: 10,
      expiresAt: expect.any(String),
    });
    const link = await screen.findByRole("group", { name: "Active Join Link" });
    expect(link).toHaveTextContent("Member only");
    await user.click(
      within(link).getByRole("button", { name: "Revoke Join Link" }),
    );
    expect(revokeJoinLink).toHaveBeenCalledWith("link-1");
  });

  it("shows sync-pending instead of claiming revocation completion", async () => {
    const user = userEvent.setup();
    const changeMember = vi.fn(async () => {
      throw new AuthorityClientError("restriction_sync_pending", 403);
    });

    render(
      <AuthorityMembersPage
        workspace={ownerWorkspace}
        currentUserId="owner-1"
        buildJoinLinkUrl={(token) =>
          `https://vibes.test/tag/join?token=${token}`
        }
        client={client({
          listMembers: vi.fn(async () => [
            {
              userId: "owner-1",
              name: "Owner",
              role: "owner" as const,
              membershipGeneration: 1,
            },
            {
              userId: "member-1",
              name: "Member",
              role: "member" as const,
              membershipGeneration: 1,
            },
          ]),
          changeMember,
        })}
      />,
      { wrapper: wrapper() },
    );

    const row = await screen.findByRole("group", { name: "Member Member" });
    await user.click(
      within(row).getByRole("button", { name: "Remove Member" }),
    );
    await user.click(screen.getByRole("button", { name: "Confirm remove" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Access is blocked. Finishing sync.",
    );
    expect(screen.queryByText(/revocation complete/i)).not.toBeInTheDocument();
  });

  it("distinguishes a committed role change from access-blocking removal sync", async () => {
    const user = userEvent.setup();
    const changeMember = vi.fn(async () => ({
      ok: true as const,
      workspaceId: "workspace-1",
      authorityVersion: 5,
      membershipGeneration: 2,
    }));

    render(
      <AuthorityMembersPage
        workspace={ownerWorkspace}
        currentUserId="owner-1"
        buildJoinLinkUrl={(token) =>
          `https://vibes.test/tag/join?token=${token}`
        }
        client={client({
          listMembers: vi.fn(async () => [
            {
              userId: "owner-1",
              name: "Owner",
              role: "owner" as const,
              membershipGeneration: 1,
            },
            {
              userId: "member-1",
              name: "Member",
              role: "member" as const,
              membershipGeneration: 1,
            },
          ]),
          changeMember,
        })}
      />,
      { wrapper: wrapper() },
    );

    const row = await screen.findByRole("group", { name: "Member Member" });
    await user.click(
      within(row).getByRole("button", {
        name: "Change Member role to admin",
      }),
    );

    expect(changeMember).toHaveBeenCalledWith("workspace-1", {
      targetUserId: "member-1",
      role: "admin",
      status: "active",
      expectedAuthorityVersion: 4,
      idempotencyKey: expect.any(String),
    });
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Role updated in VIBES. Finishing permission sync.",
    );
  });
});

describe("invite and Join Link acceptance", () => {
  it("renders the VIBES wrong-account recovery state for a targeted invitation", async () => {
    const user = userEvent.setup();
    render(
      <AuthorityInvitePage
        token="invite-token"
        client={client({
          acceptInvitation: vi.fn(async () => {
            throw new AuthorityClientError("wrong_account", 403);
          }),
        })}
        onReady={vi.fn()}
      />,
      { wrapper: wrapper() },
    );

    await user.click(screen.getByRole("button", { name: "Accept invitation" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "different VIBES account",
    );
    expect(screen.queryByText(/Multica login/i)).not.toBeInTheDocument();
  });

  it("claims a Member-only Join Link and waits for projection readiness", async () => {
    const user = userEvent.setup();
    const onReady = vi.fn();
    render(
      <AuthorityJoinPage
        token="join-token"
        client={client({
          claimJoinLink: vi.fn(async () => ({
            ok: true as const,
            workspaceId: "workspace-1",
            authorityVersion: 5,
            membershipGeneration: 2,
          })),
        })}
        onReady={onReady}
      />,
      { wrapper: wrapper() },
    );

    await user.click(screen.getByRole("button", { name: "Join Workspace" }));
    await waitFor(() => expect(onReady).toHaveBeenCalledWith(ownerWorkspace));
  });
});

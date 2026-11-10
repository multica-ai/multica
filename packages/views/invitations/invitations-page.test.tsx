import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";

const {
  auth,
  navigate,
  logout,
  refreshMe,
  acceptInvitation,
  declineInvitation,
  getInvitation,
  markOnboardingComplete,
  listMyInvitations,
  listWorkspaces,
} = vi.hoisted(() => ({
  auth: { user: null as { onboarded_at: string | null } | null },
  navigate: vi.fn(),
  logout: vi.fn(),
  refreshMe: vi.fn(),
  acceptInvitation: vi.fn(),
  declineInvitation: vi.fn(),
  getInvitation: vi.fn(),
  markOnboardingComplete: vi.fn(),
  listMyInvitations: vi.fn(),
  listWorkspaces: vi.fn(),
}));

// Mocked at the context module rather than the barrel so <AppLink> stays the
// real component and its click contract is what the test exercises.
vi.mock("../navigation/context", () => ({
  useNavigation: () => ({ push: navigate, replace: navigate }),
}));

vi.mock("../auth", () => ({
  useLogout: () => logout,
}));

vi.mock("../platform", () => ({
  DragStrip: () => null,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: auth.user, refreshMe };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ user: auth.user, refreshMe }),
    },
  ),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
    acceptInvitation,
    declineInvitation,
    getInvitation,
    markOnboardingComplete,
    listMyInvitations,
    listWorkspaces,
  },
}));

import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enInvite from "../locales/en/invite.json";
import { InvitationsPage, type InvitationsPageProps } from "./invitations-page";

const TEST_RESOURCES = { en: { common: enCommon, invite: enInvite } };

function renderPage(props: InvitationsPageProps = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <InvitationsPage {...props} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const mkInvite = (id: string, wsId: string, wsName: string, overrides = {}) => ({
  id,
  workspace_id: wsId,
  inviter_id: "u-2",
  invitee_email: "x@example.com",
  invitee_user_id: null,
  role: "member" as const,
  status: "pending" as const,
  created_at: "",
  updated_at: "",
  expires_at: "2999-01-01T00:00:00Z",
  workspace_name: wsName,
  inviter_name: "Alice",
  ...overrides,
});

const mkWs = (id: string, slug: string) => ({
  id,
  name: slug,
  slug,
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: slug.toUpperCase(),
  avatar_url: null,
  created_at: "",
  updated_at: "",
});

const row = (name: string) => screen.getByText(name).closest("li")!;

describe("InvitationsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.user = { onboarded_at: null };
    refreshMe.mockResolvedValue(undefined);
    acceptInvitation.mockResolvedValue({});
    declineInvitation.mockResolvedValue(undefined);
    markOnboardingComplete.mockResolvedValue({});
    listWorkspaces.mockResolvedValue([]);
  });

  it("lists every pending invitation with its own actions and a skip link", async () => {
    listMyInvitations.mockResolvedValue([
      mkInvite("inv-1", "ws-1", "Acme"),
      mkInvite("inv-2", "ws-2", "Beta Corp"),
    ]);
    renderPage();
    await waitFor(() => screen.getByText("Acme"));
    expect(within(row("Acme")).getByRole("button", { name: "Join" })).toBeInTheDocument();
    expect(within(row("Beta Corp")).getByRole("button", { name: "Decline" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("link", { name: /skip/i }));
    expect(navigate).toHaveBeenCalledWith("/onboarding");
    expect(acceptInvitation).not.toHaveBeenCalled();
  });

  it("joining a row accepts it, marks onboarded, and enters that workspace", async () => {
    listMyInvitations.mockResolvedValue([
      mkInvite("inv-1", "ws-1", "Acme"),
      mkInvite("inv-2", "ws-2", "Beta"),
    ]);
    listWorkspaces.mockResolvedValue([mkWs("ws-1", "acme"), mkWs("ws-2", "beta")]);
    renderPage();
    await waitFor(() => screen.getByText("Beta"));

    fireEvent.click(within(row("Beta")).getByRole("button", { name: "Join" }));

    await waitFor(() => {
      expect(acceptInvitation).toHaveBeenCalledWith("inv-2");
      expect(markOnboardingComplete).toHaveBeenCalledWith({
        completion_path: "invite_accept",
        workspace_id: "ws-2",
      });
      expect(refreshMe).toHaveBeenCalled();
      expect(navigate).toHaveBeenCalledWith("/beta/issues");
    });
    expect(acceptInvitation).toHaveBeenCalledTimes(1);
  });

  it("declining a row drops it and keeps the user on the page", async () => {
    listMyInvitations
      .mockResolvedValueOnce([mkInvite("inv-1", "ws-1", "Acme"), mkInvite("inv-2", "ws-2", "Beta")])
      .mockResolvedValue([mkInvite("inv-2", "ws-2", "Beta")]);
    renderPage();
    await waitFor(() => screen.getByText("Acme"));

    fireEvent.click(within(row("Acme")).getByRole("button", { name: "Decline" }));

    await waitFor(() => expect(screen.queryByText("Acme")).toBeNull());
    expect(declineInvitation).toHaveBeenCalledWith("inv-1");
    expect(screen.getByText("Beta")).toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalled();
  });

  it("pins and highlights the linked invitation for an onboarded user", async () => {
    auth.user = { onboarded_at: "2026-01-01T00:00:00Z" };
    const linked = mkInvite("inv-2", "ws-2", "Beta");
    listMyInvitations.mockResolvedValue([mkInvite("inv-1", "ws-1", "Acme"), linked]);
    getInvitation.mockResolvedValue(linked);
    listWorkspaces.mockResolvedValue([mkWs("ws-0", "home"), mkWs("ws-2", "beta")]);
    renderPage({ invitationId: "inv-2", onBack: vi.fn() });
    await waitFor(() => screen.getByText("Beta"));

    const items = screen.getAllByRole("listitem");
    expect(within(items[0]!).getByText("Beta")).toBeInTheDocument();
    expect(items[0]).toHaveAttribute("aria-current", "true");
    expect(items[1]).not.toHaveAttribute("aria-current");
    expect(screen.getByRole("button", { name: "Back" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /skip/i })).toBeNull();

    fireEvent.click(within(items[0]!).getByRole("button", { name: "Join" }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/beta/issues"));
    expect(markOnboardingComplete).not.toHaveBeenCalled();
  });

  it("explains a linked invitation that can no longer be joined above the others", async () => {
    listMyInvitations.mockResolvedValue([mkInvite("inv-1", "ws-1", "Acme")]);
    getInvitation.mockResolvedValue(mkInvite("inv-9", "ws-9", "Gone", { status: "declined" }));
    renderPage({ invitationId: "inv-9" });

    await waitFor(() =>
      expect(screen.getByText("This invitation has already been declined.")).toBeInTheDocument(),
    );
    expect(screen.getByText("Acme")).toBeInTheDocument();
  });

  it("sends an onboarded user with nothing left to join back to the dashboard", async () => {
    auth.user = { onboarded_at: "2026-01-01T00:00:00Z" };
    listMyInvitations.mockResolvedValue([]);
    getInvitation.mockRejectedValue(new Error("invitation does not belong to you"));
    listWorkspaces.mockResolvedValue([mkWs("ws-0", "home")]);
    renderPage({ invitationId: "inv-9" });

    await waitFor(() =>
      expect(screen.getByText(/doesn't belong to your account/)).toBeInTheDocument(),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Go to dashboard" })).toHaveAttribute(
        "href",
        "/home/issues",
      ),
    );
  });

  it("empty list falls through to onboarding via Continue button", async () => {
    listMyInvitations.mockResolvedValue([]);
    renderPage();

    await waitFor(() =>
      screen.getByRole("button", { name: /continue to setup/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: /continue to setup/i }));
    expect(navigate).toHaveBeenCalledWith("/onboarding");
  });
});

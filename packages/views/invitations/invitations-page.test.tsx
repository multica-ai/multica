import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";

const {
  navigate,
  logout,
  refreshMe,
  acceptInvitation,
  markOnboardingComplete,
  listMyInvitations,
  listWorkspaces,
} = vi.hoisted(() => ({
  navigate: vi.fn(),
  logout: vi.fn(),
  refreshMe: vi.fn(),
  acceptInvitation: vi.fn(),
  markOnboardingComplete: vi.fn(),
  listMyInvitations: vi.fn(),
  listWorkspaces: vi.fn(),
}));

vi.mock("../navigation", () => ({
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
      const state = { refreshMe };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({ refreshMe }),
    },
  ),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    acceptInvitation,
    markOnboardingComplete,
    listMyInvitations,
    listWorkspaces,
  },
}));

import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enInvite from "../locales/en/invite.json";
import { InvitationsPage } from "./invitations-page";

const TEST_RESOURCES = { en: { common: enCommon, invite: enInvite } };

function renderWithClient(client: QueryClient = new QueryClient()) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={client}>
        <InvitationsPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const mkInvite = (id: string, wsId: string, wsName: string) => ({
  id,
  workspace_id: wsId,
  inviter_id: "u-2",
  invitee_email: "x@example.com",
  invitee_user_id: null,
  role: "member" as const,
  status: "pending" as const,
  created_at: "",
  updated_at: "",
  expires_at: "",
  workspace_name: wsName,
  inviter_name: "Alice",
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
  created_at: "",
  updated_at: "",
});

describe("InvitationsPage", () => {
  beforeEach(() => {
    navigate.mockReset();
    logout.mockReset();
    refreshMe.mockReset();
    acceptInvitation.mockReset();
    markOnboardingComplete.mockReset();
    listMyInvitations.mockReset();
    listWorkspaces.mockReset();
    refreshMe.mockResolvedValue(undefined);
    acceptInvitation.mockResolvedValue({});
    markOnboardingComplete.mockResolvedValue({});
  });

  it("renders pending invitations with workspace names", async () => {
    listMyInvitations.mockResolvedValue([
      mkInvite("inv-1", "ws-1", "Acme"),
      mkInvite("inv-2", "ws-2", "Beta Corp"),
    ]);
    renderWithClient();
    await waitFor(() => {
      expect(screen.getByText("Acme")).toBeInTheDocument();
      expect(screen.getByText("Beta Corp")).toBeInTheDocument();
    });
  });

  it("with no selections, submitting routes to /onboarding", async () => {
    listMyInvitations.mockResolvedValue([mkInvite("inv-1", "ws-1", "Acme")]);
    renderWithClient();
    await waitFor(() => screen.getByText("Acme"));
    fireEvent.click(screen.getByRole("button", { name: /skip/i }));
    expect(navigate).toHaveBeenCalledWith("/onboarding");
    // Empty submit doesn't accept anything or touch onboarding state.
    expect(acceptInvitation).not.toHaveBeenCalled();
    expect(markOnboardingComplete).not.toHaveBeenCalled();
  });

  it("accepts selected invitations, marks onboarded, navigates to first ws", async () => {
    listMyInvitations.mockResolvedValue([
      mkInvite("inv-1", "ws-1", "Acme"),
      mkInvite("inv-2", "ws-2", "Beta"),
    ]);
    listWorkspaces.mockResolvedValue([mkWs("ws-1", "acme"), mkWs("ws-2", "beta")]);
    renderWithClient();

    await waitFor(() => screen.getByText("Acme"));
    // Select Acme via its label/checkbox row.
    fireEvent.click(screen.getByText("Acme"));

    fireEvent.click(screen.getByRole("button", { name: /join 1 workspace/i }));

    await waitFor(() => {
      expect(acceptInvitation).toHaveBeenCalledWith("inv-1");
      expect(markOnboardingComplete).toHaveBeenCalledWith({
        completion_path: "invite_accept",
        workspace_id: "ws-1",
      });
      expect(refreshMe).toHaveBeenCalled();
      expect(navigate).toHaveBeenCalledWith("/acme/issues");
    });
  });

  it("empty list falls through to onboarding via Continue button", async () => {
    listMyInvitations.mockResolvedValue([]);
    renderWithClient();

    await waitFor(() =>
      screen.getByRole("button", { name: /continue to setup/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /continue to setup/i }),
    );
    expect(navigate).toHaveBeenCalledWith("/onboarding");
  });
});

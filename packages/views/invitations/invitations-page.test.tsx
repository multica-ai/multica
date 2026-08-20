import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";

const {
  navigate,
  logout,
  acceptInvitation,
  listMyInvitations,
  listWorkspaces,
} = vi.hoisted(() => ({
  navigate: vi.fn(),
  logout: vi.fn(),
  acceptInvitation: vi.fn(),
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

vi.mock("@multica/core/api", () => ({
  api: {
    acceptInvitation,
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
  avatar_url: null,
  created_at: "",
  updated_at: "",
});

describe("InvitationsPage", () => {
  beforeEach(() => {
    navigate.mockReset();
    logout.mockReset();
    acceptInvitation.mockReset();
    listMyInvitations.mockReset();
    listWorkspaces.mockReset();
    acceptInvitation.mockResolvedValue({});
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

  it("with no selections, submitting routes to authority Workspace creation", async () => {
    listMyInvitations.mockResolvedValue([mkInvite("inv-1", "ws-1", "Acme")]);
    renderWithClient();
    await waitFor(() => screen.getByText("Acme"));
    fireEvent.click(screen.getByRole("button", { name: /skip/i }));
    expect(navigate).toHaveBeenCalledWith("/workspaces/new");
    expect(acceptInvitation).not.toHaveBeenCalled();
  });

  it("accepts selected invitations and navigates to the first workspace", async () => {
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
      expect(navigate).toHaveBeenCalledWith("/acme/issues");
    });
  });

  it("empty list falls through to Workspace creation via Continue", async () => {
    listMyInvitations.mockResolvedValue([]);
    renderWithClient();

    await waitFor(() =>
      screen.getByRole("button", { name: /continue to setup/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /continue to setup/i }),
    );
    expect(navigate).toHaveBeenCalledWith("/workspaces/new");
  });
});

import type { ReactElement, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "@testing-library/jest-dom/vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enInvite from "../locales/en/invite.json";

const mockGetInvitation = vi.hoisted(() => vi.fn());
const mockAcceptInvitation = vi.hoisted(() => vi.fn());
const mockDeclineInvitation = vi.hoisted(() => vi.fn());
const mockMarkOnboardingComplete = vi.hoisted(() => vi.fn());
const mockRefreshMe = vi.hoisted(() => vi.fn());
const mockPush = vi.hoisted(() => vi.fn());
const mockLogout = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());
const mockFetchQuery = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey, queryFn }: { queryKey: string[]; queryFn: () => unknown }) => {
    if (queryKey[0] === "invitation") {
      return { data: mockGetInvitation(), isLoading: false, error: null };
    }
    return { data: [], isLoading: false, error: null, queryFn };
  },
  useQueryClient: () => ({
    fetchQuery: mockFetchQuery,
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getInvitation: mockGetInvitation,
    acceptInvitation: mockAcceptInvitation,
    declineInvitation: mockDeclineInvitation,
    markOnboardingComplete: mockMarkOnboardingComplete,
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: {
    getState: () => ({ refreshMe: mockRefreshMe }),
  },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { myInvitations: () => ["workspaces", "invitations"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces", "list"] }),
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({ issues: () => `/${slug}/issues` }),
  },
  resolvePostAuthDestination: () => "/onboarding",
  useHasOnboarded: () => true,
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("../auth", () => ({
  useLogout: () => mockLogout,
}));

vi.mock("../platform", () => ({
  DragStrip: () => null,
}));

import { InvitePage } from "./invite-page";

const TEST_RESOURCES = {
  en: { common: enCommon, invite: enInvite },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderInvite(ui: ReactElement) {
  return render(ui, { wrapper: I18nWrapper });
}

const PENDING_INVITATION = {
  workspace_id: "workspace-1",
  workspace_name: "Acme",
  inviter_name: "Ada Lovelace",
  inviter_email: "ada@example.com",
  role: "member",
  status: "pending",
};

describe("InvitePage", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetInvitation.mockReturnValue(PENDING_INVITATION);
    mockAcceptInvitation.mockResolvedValue(undefined);
    mockDeclineInvitation.mockResolvedValue(undefined);
    mockMarkOnboardingComplete.mockResolvedValue(undefined);
    mockRefreshMe.mockResolvedValue(undefined);
    mockFetchQuery.mockResolvedValue([]);
  });

  it("shows a pending invitation with the available actions", () => {
    renderInvite(<InvitePage invitationId="invite-1" />);

    expect(screen.getByRole("heading", { name: "Join Acme" })).toBeInTheDocument();
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("invited you to join as a member.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Decline" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Accept & Join" })).toBeEnabled();
  });

  it("declines the invitation and shows the confirmation state", async () => {
    renderInvite(<InvitePage invitationId="invite-1" />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Decline" }));

    await waitFor(() => {
      expect(mockDeclineInvitation).toHaveBeenCalledWith("invite-1");
      expect(screen.getByRole("heading", { name: "Invitation declined" })).toBeInTheDocument();
    });
    expect(mockInvalidateQueries).toHaveBeenCalledWith({
      queryKey: ["workspaces", "invitations"],
    });
  });

  it("reports an accept failure without leaving the invitation page", async () => {
    mockAcceptInvitation.mockRejectedValueOnce(new Error("Invitation expired"));
    renderInvite(<InvitePage invitationId="invite-1" />);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Accept & Join" }));

    await waitFor(() => {
      expect(screen.getByText("Invitation expired")).toBeInTheDocument();
    });
    expect(mockMarkOnboardingComplete).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("shows the already accepted state without mutation actions", () => {
    mockGetInvitation.mockReturnValue({
      ...PENDING_INVITATION,
      status: "accepted",
    });

    renderInvite(<InvitePage invitationId="invite-1" />);

    expect(
      screen.getByText("This invitation has already been accepted."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Decline" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Accept & Join" })).not.toBeInTheDocument();
  });
});

import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enInvite from "../locales/en/invite.json";

const TEST_RESOURCES = { en: { invite: enInvite } };

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const { mockGetInvitation, mockAcceptInvitation, mockDeclineInvitation, mockListWorkspaces } =
  vi.hoisted(() => ({
    mockGetInvitation: vi.fn(),
    mockAcceptInvitation: vi.fn(),
    mockDeclineInvitation: vi.fn(),
    mockListWorkspaces: vi.fn().mockResolvedValue([]),
  }));

vi.mock("@multica/core/api", () => ({
  api: {
    getInvitation: mockGetInvitation,
    acceptInvitation: mockAcceptInvitation,
    declineInvitation: mockDeclineInvitation,
    listWorkspaces: mockListWorkspaces,
  },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("../auth", () => ({
  useLogout: () => vi.fn(),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { user: { onboarded_at: "2026-01-01" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ refreshMe: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return { ...actual, useHasOnboarded: () => true };
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { InvitePage } from "./invite-page";

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={qc}>
        <InvitePage invitationId="inv-1" />
      </QueryClientProvider>
    </I18nWrapper>,
  );
}

describe("InvitePage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders Accept + Decline for a targeted invitation", async () => {
    mockGetInvitation.mockResolvedValue({
      id: "inv-1",
      workspace_id: "ws-1",
      inviter_id: "u-1",
      invitee_email: "alice@example.com",
      invitee_user_id: null,
      role: "member",
      status: "pending",
      created_at: "",
      updated_at: "",
      expires_at: "",
      shareable: false,
      max_uses: null,
      use_count: 0,
      workspace_name: "Acme",
      inviter_name: "Bob",
    });

    renderPage();

    expect(await screen.findByRole("button", { name: /accept & join/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /decline/i })).toBeInTheDocument();
    // Targeted flow shows the inviter's name in the body copy.
    expect(screen.getByText(/bob/i)).toBeInTheDocument();
  });

  it("renders a single Join button (no Decline) for a shareable link", async () => {
    mockGetInvitation.mockResolvedValue({
      id: "inv-1",
      workspace_id: "ws-1",
      inviter_id: "u-1",
      invitee_email: null,
      invitee_user_id: null,
      role: "member",
      status: "pending",
      created_at: "",
      updated_at: "",
      expires_at: "",
      shareable: true,
      max_uses: 5,
      use_count: 2,
      workspace_name: "Acme",
      inviter_name: "Bob",
    });

    renderPage();

    expect(await screen.findByRole("button", { name: /join workspace/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /decline/i })).not.toBeInTheDocument();
    // Shareable flow intentionally omits the inviter's name — the link may
    // have been passed through multiple hands, and naming the original
    // creator would be misleading.
    expect(screen.queryByText(/bob/i)).not.toBeInTheDocument();
  });

  it("shows used-up copy when a shareable link has been exhausted", async () => {
    mockGetInvitation.mockResolvedValue({
      id: "inv-1",
      workspace_id: "ws-1",
      inviter_id: "u-1",
      invitee_email: null,
      invitee_user_id: null,
      role: "member",
      status: "accepted",
      created_at: "",
      updated_at: "",
      expires_at: "",
      shareable: true,
      max_uses: 2,
      use_count: 2,
      workspace_name: "Acme",
    });

    renderPage();

    expect(await screen.findByText(/used up/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /join workspace/i })).not.toBeInTheDocument();
  });
});

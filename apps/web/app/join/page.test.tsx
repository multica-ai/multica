import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "@multica/views/locales/en/common.json";
import enAuth from "@multica/views/locales/en/auth.json";
import enSettings from "@multica/views/locales/en/settings.json";
import { ApiError } from "@multica/core/api";
import type { ReactNode } from "react";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const {
  mockGetShareLinkInfo,
  mockJoinByShareLink,
  mockListWorkspaces,
  mockPush,
  searchParamsState,
  authStateRef,
} = vi.hoisted(() => ({
  mockGetShareLinkInfo: vi.fn(),
  mockJoinByShareLink: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockPush: vi.fn(),
  searchParamsState: { params: new URLSearchParams() },
  authStateRef: {
    state: {
      user: null as null | { id: string; email: string },
      isLoading: false,
    },
  },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush, replace: vi.fn() }),
  usePathname: () => "/join",
  useSearchParams: () => searchParamsState.params,
}));

vi.mock("@multica/core/auth", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/auth")>(
    "@multica/core/auth",
  );
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) =>
      selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      getShareLinkInfo: mockGetShareLinkInfo,
      joinByShareLink: mockJoinByShareLink,
      listWorkspaces: mockListWorkspaces,
    },
  };
});

import JoinPage from "./page";

const TEST_INFO = {
  workspace_name: "Acme",
  workspace_slug: "acme",
  creator_name: "Alice",
  role: "member",
};

describe("JoinPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams({ code: "abc123" });
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockGetShareLinkInfo.mockResolvedValue(TEST_INFO);
    mockListWorkspaces.mockResolvedValue([]);
  });

  it("shows the workspace preview without joining", async () => {
    render(<JoinPage />, { wrapper: createWrapper() });

    await waitFor(() =>
      expect(screen.getByText(/You're invited to Acme/)).toBeInTheDocument(),
    );
    expect(screen.getByText("Invited by Alice")).toBeInTheDocument();
    expect(mockJoinByShareLink).not.toHaveBeenCalled();
  });

  it("shows an Administrator badge when the link grants admin access", async () => {
    mockGetShareLinkInfo.mockResolvedValue({
      ...TEST_INFO,
      role: "admin",
    });

    render(<JoinPage />, { wrapper: createWrapper() });

    await waitFor(() =>
      expect(
        screen.getByText("You'll join this workspace as"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("Administrator")).toBeInTheDocument();
  });

  it("shows a Member badge when the link grants member access", async () => {
    mockGetShareLinkInfo.mockResolvedValue({
      ...TEST_INFO,
      role: "member",
    });

    render(<JoinPage />, { wrapper: createWrapper() });

    await waitFor(() =>
      expect(
        screen.getByText("You'll join this workspace as"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("Member")).toBeInTheDocument();
  });

  it("shows an error when the link is invalid", async () => {
    mockGetShareLinkInfo.mockRejectedValue(new Error("not found"));

    render(<JoinPage />, { wrapper: createWrapper() });

    await waitFor(() =>
      expect(
        screen.getByText("This invite link is invalid or has expired."),
      ).toBeInTheDocument(),
    );
    expect(mockJoinByShareLink).not.toHaveBeenCalled();
  });

  it("sends unauthenticated users to login when they click join", async () => {
    const user = userEvent.setup();
    authStateRef.state.user = null;
    render(<JoinPage />, { wrapper: createWrapper() });

    await user.click(
      await screen.findByRole("button", { name: "Log In to Join" }),
    );
    expect(mockPush).toHaveBeenCalledWith(
      "/login?next=" + encodeURIComponent("/join?code=abc123"),
    );
    expect(mockJoinByShareLink).not.toHaveBeenCalled();
  });

  it("joins only when an authenticated user clicks the button", async () => {
    const user = userEvent.setup();
    authStateRef.state.user = { id: "u1", email: "a@b.com" };
    mockJoinByShareLink.mockResolvedValue({
      member: { id: "m1" },
      workspace_id: "ws1",
      workspace_slug: "acme",
    });

    render(<JoinPage />, { wrapper: createWrapper() });

    await user.click(
      await screen.findByRole("button", { name: "Join Workspace" }),
    );
    await waitFor(() => expect(mockJoinByShareLink).toHaveBeenCalledTimes(1));
    // The join succeeded — the page should transition to the Joined state
    // (navigation happens after a short delay).
    await waitFor(() =>
      expect(screen.getByText("Joined!")).toBeInTheDocument(),
    );
  });

  it("redirects to the first workspace when already a member", async () => {
    const user = userEvent.setup();
    authStateRef.state.user = { id: "u1", email: "a@b.com" };
    mockJoinByShareLink.mockRejectedValue(
      new Error("you are already a member of this workspace"),
    );
    // The account's first workspace differs from the invited one — the page
    // must route to the workspace THIS invite points at (acme), not the first.
    mockListWorkspaces.mockResolvedValue([{ id: "ws0", slug: "other" }]);

    render(<JoinPage />, { wrapper: createWrapper() });

    await user.click(
      await screen.findByRole("button", { name: "Join Workspace" }),
    );
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith("/acme/issues"));
  });

  it("shows an error when join fails for another reason", async () => {
    const user = userEvent.setup();
    authStateRef.state.user = { id: "u1", email: "a@b.com" };
    mockJoinByShareLink.mockRejectedValue(
      new Error("share link not found or expired"),
    );

    render(<JoinPage />, { wrapper: createWrapper() });

    await user.click(
      await screen.findByRole("button", { name: "Join Workspace" }),
    );
    await waitFor(() =>
      expect(
        screen.getByText("share link not found or expired"),
      ).toBeInTheDocument(),
    );
  });

  it.each([
    [
      "seat_capacity_full",
      "All purchased member seats are in use. Ask a workspace admin to add a seat before trying again.",
    ],
    [
      "seat_capacity_unavailable",
      "Member capacity could not be verified. Please try again.",
    ],
  ])("maps %s to a user-facing capacity message", async (code, message) => {
    const user = userEvent.setup();
    authStateRef.state.user = { id: "u1", email: "a@b.com" };
    mockJoinByShareLink.mockRejectedValue(
      new ApiError("server fallback", 409, "Conflict", { code }),
    );

    render(<JoinPage />, { wrapper: createWrapper() });

    await user.click(
      await screen.findByRole("button", { name: "Join Workspace" }),
    );
    await waitFor(() => expect(screen.getByText(message)).toBeInTheDocument());
  });
});

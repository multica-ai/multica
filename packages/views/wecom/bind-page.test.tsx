import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

// bind-page.test.tsx — the /wecom/bind redeem page. Mirrors lark/bind-page.test.tsx
// and adds the WeCom error-code → copy mapping (410 / 409 / 403 → distinct
// user-facing reasons), so a broken redeem never silently shows the wrong state.
//
// Co-authored analysis: seacen (PR #5833 review).

const TEST_RESOURCES = { en: { common: enCommon } };

const mockAuthState = vi.hoisted(() => ({
  user: null as { id: string; email: string } | null,
  isLoading: false,
}));
const mockNavigatePush = vi.hoisted(() => vi.fn());
const mockRedeemToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: typeof mockAuthState) => unknown) => (sel ? sel(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  );
  return { useAuthStore };
});

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockNavigatePush }),
}));

vi.mock("@multica/core/api", () => ({
  api: { redeemWecomBindingToken: mockRedeemToken },
}));

import { WecomBindPage } from "./bind-page";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage(token: string | null) {
  return render(<WecomBindPage token={token} />, { wrapper: I18nWrapper });
}

describe("WecomBindPage", () => {
  beforeEach(() => {
    mockAuthState.user = null;
    mockAuthState.isLoading = false;
    mockNavigatePush.mockReset();
    mockRedeemToken.mockReset();
  });

  it("shows the linking text while auth is still loading (not needs-auth)", () => {
    mockAuthState.isLoading = true;
    renderPage("tok123");
    expect(screen.getByText(/linking your account/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /sign in/i })).toBeNull();
  });

  it("shows needs-auth UI when auth finishes loading and user is null", () => {
    renderPage("tok123");
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
    // Must NOT have attempted a redeem without a session.
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it("redeems immediately when the user is already logged in, then shows success", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({ workspace_id: "ws1", installation_id: "inst1", wecom_user_id: "T_x" });
    renderPage("tok123");
    await waitFor(() => expect(mockRedeemToken).toHaveBeenCalledWith("tok123"));
    await waitFor(() => expect(screen.getByText(/you're linked/i)).toBeInTheDocument());
  });

  it("sign-in navigates with ?next= carrying the token (not ?redirect=)", () => {
    renderPage("mytoken");
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(mockNavigatePush).toHaveBeenCalledTimes(1);
    const url = mockNavigatePush.mock.calls[0]?.[0] as string;
    expect(url).toContain("?next=");
    expect(url).not.toContain("?redirect=");
    expect(url).toContain(encodeURIComponent("mytoken"));
  });

  it("shows the missing-token error when token is null (and never calls redeem)", () => {
    renderPage(null);
    expect(screen.getByText(/missing its token/i)).toBeInTheDocument();
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  // Error-code → copy mapping: the page derives the reason from the thrown
  // error's message (status code or phrase). Each must land on distinct copy.
  it.each([
    ["410", /invalid or expired/i],
    ["expired", /invalid or expired/i],
    ["409", /already linked to a different Multica user/i],
    ["already bound", /already linked to a different Multica user/i],
    ["403", /isn't a member of this workspace/i],
    ["workspace member", /isn't a member of this workspace/i],
    ["something exploded", /something went wrong/i],
  ])("maps a %s failure to the right message", async (msg, expected) => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockRejectedValue(new Error(msg));
    renderPage("tok123");
    await waitFor(() => expect(screen.getByText(expected)).toBeInTheDocument());
  });
});

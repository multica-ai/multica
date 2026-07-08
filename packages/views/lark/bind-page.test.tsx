import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

const TEST_RESOURCES = { en: { common: enCommon } };

// ---------------------------------------------------------------------------
// Hoisted mutable auth state — lets individual tests set different scenarios
// ---------------------------------------------------------------------------
const mockAuthState = vi.hoisted(() => ({
  user: null as { id: string; email: string } | null,
  isLoading: false,
}));

const mockNavigatePush = vi.hoisted(() => vi.fn());
const mockRedeemToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: typeof mockAuthState) => unknown) =>
      sel ? sel(mockAuthState) : mockAuthState,
    { getState: () => mockAuthState },
  );
  return { useAuthStore };
});

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockNavigatePush }),
}));

vi.mock("@multica/core/api", () => ({
  api: { redeemLarkBindingToken: mockRedeemToken },
}));

import { LarkBindPage } from "./bind-page";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage(token: string | null) {
  return render(<LarkBindPage token={token} />, { wrapper: I18nWrapper });
}

describe("LarkBindPage", () => {
  beforeEach(() => {
    mockAuthState.user = null;
    mockAuthState.isLoading = false;
    mockNavigatePush.mockReset();
    mockRedeemToken.mockReset();
  });

  it("shows redeeming text while auth is still loading (not needs-auth)", () => {
    mockAuthState.isLoading = true;
    mockAuthState.user = null;
    renderPage("tok123");
    expect(screen.getByText(/redeeming binding token/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /sign in/i })).toBeNull();
  });

  it("shows needs-auth UI when auth finishes loading and user is null", () => {
    mockAuthState.isLoading = false;
    mockAuthState.user = null;
    renderPage("tok123");
    expect(
      screen.getByRole("button", { name: /sign in/i }),
    ).toBeInTheDocument();
  });

  it("starts redemption immediately when user is already logged in", async () => {
    mockAuthState.isLoading = false;
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "ws1",
      installation_id: "inst1",
    });
    renderPage("tok123");
    await waitFor(() => {
      expect(mockRedeemToken).toHaveBeenCalledWith("tok123");
    });
  });

  it("shows success state after successful redemption", async () => {
    mockAuthState.isLoading = false;
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "ws1",
      installation_id: "inst1",
    });
    renderPage("tok123");
    await waitFor(() => {
      expect(screen.getByText(/you're bound/i)).toBeInTheDocument();
    });
  });

  it("sign-in button navigates with ?next= parameter (not ?redirect=)", () => {
    mockAuthState.isLoading = false;
    mockAuthState.user = null;
    renderPage("mytoken");
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(mockNavigatePush).toHaveBeenCalledTimes(1);
    const url: string = mockNavigatePush.mock.calls[0]?.[0] as string;
    expect(url).toContain("?next=");
    expect(url).not.toContain("?redirect=");
    expect(url).toContain(encodeURIComponent("mytoken"));
  });

  it("shows missing token error when token is null", () => {
    renderPage(null);
    expect(
      screen.getByText(/missing its binding token/i),
    ).toBeInTheDocument();
  });
});

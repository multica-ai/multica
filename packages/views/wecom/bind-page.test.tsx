import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

const TEST_RESOURCES = { en: { common: enCommon } };

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

  it("shows redeeming text while auth is still loading", () => {
    mockAuthState.isLoading = true;
    renderPage("tok123");
    expect(screen.getByText(/redeeming binding token/i)).toBeInTheDocument();
  });

  it("shows needs-auth UI when user is null", () => {
    renderPage("tok123");
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("redeems when user is logged in", async () => {
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

  it("shows success after redemption", async () => {
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

  it("sign-in navigates with ?next=", () => {
    renderPage("mytoken");
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    const url: string = mockNavigatePush.mock.calls[0]?.[0] as string;
    expect(url).toContain("?next=");
    expect(url).toContain(encodeURIComponent("/wecom/bind?token=mytoken"));
  });

  it("shows missing token error", () => {
    renderPage(null);
    expect(screen.getByText(/missing its binding token/i)).toBeInTheDocument();
  });
});

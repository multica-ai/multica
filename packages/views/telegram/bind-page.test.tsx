import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
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
    (selector?: (state: typeof mockAuthState) => unknown) =>
      selector ? selector(mockAuthState) : mockAuthState,
    { getState: () => mockAuthState },
  );
  return { useAuthStore };
});

vi.mock("../navigation/context", () => ({
  useNavigation: () => ({ push: mockNavigatePush }),
  useOptionalNavigation: () => ({ push: mockNavigatePush }),
}));

vi.mock("@multica/core/api", () => ({
  api: { redeemTelegramBindingToken: mockRedeemToken },
}));

import { TelegramBindPage } from "./bind-page";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage(token: string | null) {
  return render(<TelegramBindPage token={token} />, { wrapper: I18nWrapper });
}

describe("TelegramBindPage", () => {
  beforeEach(() => {
    mockAuthState.user = null;
    mockAuthState.isLoading = false;
    mockNavigatePush.mockReset();
    mockRedeemToken.mockReset();
  });

  it("shows linking state while authentication is loading", () => {
    mockAuthState.isLoading = true;
    renderPage("tok123");
    expect(screen.getByText(/linking your account/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /sign in/i })).toBeNull();
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it("requires sign-in before redeeming", () => {
    renderPage("tok123");
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it("preserves the token in the login next parameter", () => {
    renderPage("token with+/reserved");
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(mockNavigatePush).toHaveBeenCalledTimes(1);
    const destination = mockNavigatePush.mock.calls[0]?.[0] as string;
    expect(destination).toContain("/login?next=");
    expect(destination).not.toContain("?redirect=");
    expect(decodeURIComponent(destination.split("next=")[1] ?? "")).toBe(
      "/telegram/bind?token=token%20with%2B%2Freserved",
    );
  });

  it("redeems immediately when signed in and shows success", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "ws1",
      installation_id: "inst1",
      telegram_user_id: "tg1",
    });

    renderPage("tok123");

    await waitFor(() => expect(mockRedeemToken).toHaveBeenCalledWith("tok123"));
    await waitFor(() => expect(screen.getByText(/you're linked/i)).toBeInTheDocument());
  });

  it("redeems a one-time token exactly once under StrictMode", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken
      .mockResolvedValueOnce({
        workspace_id: "ws1",
        installation_id: "inst1",
        telegram_user_id: "tg1",
      })
      .mockRejectedValueOnce(new Error("410 binding token invalid or expired"));

    render(
      <StrictMode>
        <TelegramBindPage token="single-use-token" />
      </StrictMode>,
      { wrapper: I18nWrapper },
    );

    await waitFor(() => expect(screen.getByText(/you're linked/i)).toBeInTheDocument());
    expect(mockRedeemToken).toHaveBeenCalledTimes(1);
  });

  it("rejects a malformed success response", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "",
      installation_id: "",
      telegram_user_id: "",
    });

    renderPage("tok123");

    await waitFor(() => expect(screen.getByText(/something went wrong/i)).toBeInTheDocument());
  });

  it("shows the missing-token error without calling the API", () => {
    renderPage(null);
    expect(screen.getByText(/missing its token/i)).toBeInTheDocument();
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it.each([
    ["410", /invalid or expired/i],
    ["expired", /invalid or expired/i],
    ["409", /already linked to a different Multica user/i],
    ["already bound", /already linked to a different Multica user/i],
    ["403", /isn't a member of this workspace/i],
    ["workspace member", /isn't a member of this workspace/i],
    ["unexpected failure", /something went wrong/i],
  ])("maps a %s redemption failure to the correct message", async (message, expected) => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockRejectedValue(new Error(message));

    renderPage("tok123");

    await waitFor(() => expect(screen.getByText(expected)).toBeInTheDocument());
  });
});

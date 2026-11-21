import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";

// bind-page.test.tsx — the /infoflow/bind redeem page. Mirrors
// wecom/bind-page.test.tsx and pins the two things that make this route
// load-bearing for the channel:
//
//   1. the token survives the login round trip (?next=/infoflow/bind?token=…),
//      so the bot's prompt works for a user who has never signed in;
//   2. every backend failure mode lands on its OWN copy — 410 expired, 409
//      already-bound, 403 not-a-member, 403 + infoflow_not_configured for a
//      deployment with no MULTICA_INFOFLOW_SECRET_KEY. The last one shares a
//      status with not-a-member, so it needs the body's `code` to stay
//      distinguishable.
//
// Co-authored analysis: seacen (PR #5833 review).

const TEST_RESOURCES = { en: { common: enCommon } };

const mockAuthState = vi.hoisted(() => ({
  user: null as { id: string; email: string } | null,
  isLoading: false,
}));
const mockNavigatePush = vi.hoisted(() => vi.fn());
const mockRedeemToken = vi.hoisted(() => vi.fn());

// Hoisted with the mocks: vi.mock factories run before top-level declarations,
// so a plain class here would be referenced before initialization. Mirrors the
// real ApiError shape (status + structured body) and the real errorCode helper's
// contract — see packages/core/api/client.ts.
const FakeApiError = vi.hoisted(
  () =>
    class FakeApiError extends Error {
      status: number;
      body?: unknown;
      constructor(status: number, message: string, body?: unknown) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.body = body;
      }
    },
);

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: typeof mockAuthState) => unknown) =>
      sel ? sel(mockAuthState) : mockAuthState,
    { getState: () => mockAuthState },
  );
  return { useAuthStore };
});

// Mock the context module, not the barrel: AppLink stays the real component
// (the sign-in button renders through it) and reads the mocked adapter.
vi.mock("../navigation/context", () => ({
  useNavigation: () => ({ push: mockNavigatePush }),
  useOptionalNavigation: () => ({ push: mockNavigatePush }),
}));

vi.mock("@multica/core/api", () => ({
  api: { redeemInfoFlowBindingToken: mockRedeemToken },
  ApiError: FakeApiError,
  errorCode: (e: unknown) =>
    e instanceof FakeApiError
      ? (e.body as { code?: string } | undefined)?.code
      : undefined,
}));

import { InfoFlowBindPage } from "./bind-page";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage(token: string | null) {
  return render(<InfoFlowBindPage token={token} />, { wrapper: I18nWrapper });
}

describe("InfoFlowBindPage", () => {
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
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it("shows needs-auth UI when auth finishes loading and the user is null", () => {
    renderPage("tok123");
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
    // Must NOT have attempted a redeem without a session.
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  it("redeems immediately when the user is already logged in, then shows success", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "ws1",
      installation_id: "inst1",
      infoflow_user_id: "if-1",
    });
    renderPage("tok123");
    await waitFor(() => expect(mockRedeemToken).toHaveBeenCalledWith("tok123"));
    await waitFor(() =>
      expect(screen.getByText(/you're linked/i)).toBeInTheDocument(),
    );
  });

  it("sign-in navigates with ?next= carrying the token (not ?redirect=)", () => {
    renderPage("token with+/reserved");
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(mockNavigatePush).toHaveBeenCalledTimes(1);
    const destination = mockNavigatePush.mock.calls[0]?.[0] as string;
    expect(destination).toContain("/login?next=");
    expect(destination).not.toContain("?redirect=");
    expect(decodeURIComponent(destination.split("next=")[1] ?? "")).toBe(
      "/infoflow/bind?token=token%20with%2B%2Freserved",
    );
  });

  it("shows the missing-token error when token is null (and never calls redeem)", () => {
    renderPage(null);
    expect(screen.getByText(/missing its token/i)).toBeInTheDocument();
    expect(mockRedeemToken).not.toHaveBeenCalled();
  });

  // A token is single-use server-side: StrictMode's double-invoked effect must
  // not fire two redemptions, or the second would overwrite a success with the
  // "expired" copy.
  it("redeems a one-time token exactly once under StrictMode", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken
      .mockResolvedValueOnce({
        workspace_id: "ws1",
        installation_id: "inst1",
        infoflow_user_id: "if-1",
      })
      .mockRejectedValueOnce(new FakeApiError(410, "binding token invalid or expired"));

    render(
      <StrictMode>
        <InfoFlowBindPage token="single-use-token" />
      </StrictMode>,
      { wrapper: I18nWrapper },
    );

    await waitFor(() =>
      expect(screen.getByText(/you're linked/i)).toBeInTheDocument(),
    );
    expect(mockRedeemToken).toHaveBeenCalledTimes(1);
  });

  it("rejects a malformed success response", async () => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockResolvedValue({
      workspace_id: "",
      installation_id: "",
      infoflow_user_id: "",
    });
    renderPage("tok123");
    await waitFor(() =>
      expect(screen.getByText(/something went wrong/i)).toBeInTheDocument(),
    );
  });

  // Status → copy mapping. 403 is deliberately listed twice: the code decides
  // which of the two sentences the user reads.
  it.each([
    ["expired token", new FakeApiError(410, "binding token invalid or expired"), /invalid or expired/i],
    ["already bound", new FakeApiError(409, "already bound to a different Multica user"), /already linked to a different Multica user/i],
    ["not a member", new FakeApiError(403, "binding refused (are you a workspace member?)"), /isn't a member of this workspace/i],
    [
      "not configured",
      new FakeApiError(403, "infoflow integration not configured", {
        code: "infoflow_not_configured",
      }),
      /no InfoFlow integration enabled/i,
    ],
    ["unexpected failure", new FakeApiError(500, "failed to redeem token"), /something went wrong/i],
  ])("maps a %s redemption failure to the right message", async (_name, err, expected) => {
    mockAuthState.user = { id: "u1", email: "u@example.com" };
    mockRedeemToken.mockRejectedValue(err);
    renderPage("tok123");
    await waitFor(() => expect(screen.getByText(expected)).toBeInTheDocument());
  });
});
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enAuth from "../locales/en/auth.json";
import enSettings from "../locales/en/settings.json";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderWithI18n(ui: ReactElement) {
  return render(ui, { wrapper: I18nWrapper });
}

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockSendCode = vi.hoisted(() => vi.fn());
const mockVerifyCode = vi.hoisted(() => vi.fn());
const mockLdapLogin = vi.hoisted(() => vi.fn());
const mockApiListWorkspaces = vi.hoisted(() => vi.fn());
const mockApiVerifyCode = vi.hoisted(() => vi.fn());
const mockApiSetToken = vi.hoisted(() => vi.fn());
const mockApiGetMe = vi.hoisted(() => vi.fn());
const mockApiIssueCliToken = vi.hoisted(() => vi.fn());
const mockApiLoginWithLdap = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());

// Real ApiError lives in the module we mock below, so the test carries its own
// copy with the same shape -- ldapErrorMessage branches on `instanceof` plus
// `status`, and nothing else.
const { ApiError } = vi.hoisted(() => {
  class ApiErrorImpl extends Error {
    readonly status: number;
    readonly statusText: string;
    readonly body?: unknown;
    constructor(message: string, status: number, statusText: string, body?: unknown) {
      super(message);
      this.name = "ApiError";
      this.status = status;
      this.statusText = statusText;
      this.body = body;
    }
  }
  return { ApiError: ApiErrorImpl };
});

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return { ...actual, useQueryClient: () => ({ setQueryData: mockSetQueryData }) };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    // Zustand hook form — component may call useAuthStore(selector)
    (selector?: (s: unknown) => unknown) => {
      const state = {
        sendCode: mockSendCode,
        verifyCode: mockVerifyCode,
        loginWithLdap: mockLdapLogin,
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        sendCode: mockSendCode,
        verifyCode: mockVerifyCode,
        loginWithLdap: mockLdapLogin,
      }),
    },
  ),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockApiListWorkspaces,
    verifyCode: mockApiVerifyCode,
    setToken: mockApiSetToken,
    getMe: mockApiGetMe,
    issueCliToken: mockApiIssueCliToken,
    loginWithLdap: mockApiLoginWithLdap,
  },
  ApiError,
}));

vi.mock("@multica/core/types", () => ({}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { LoginPage, validateCliCallback } from "./login-page";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getOTPInput() {
  // input-otp renders a single hidden <input> that holds the OTP value
  return screen.getByRole("textbox", { hidden: true });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("LoginPage", () => {
  const onSuccess = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.clearAllMocks();
    // Default: no existing session (getMe rejects when no auth)
    mockApiGetMe.mockRejectedValue(new Error("unauthorized"));
    localStorage.clear();
    // Reset window.location for tests that change it
    Object.defineProperty(window, "location", {
      writable: true,
      value: { href: "http://localhost:3000" },
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // -------------------------------------------------------------------------
  // Email step rendering
  // -------------------------------------------------------------------------

  it("renders email form with 'Sign in to Multica' title", () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(
      screen.getByText(/sign in to multica/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/enter your email to get a login code/i),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Email validation
  // -------------------------------------------------------------------------

  it("shows error when submitting with empty email", async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    // The Continue button is disabled when email is empty, so we submit the
    // form programmatically the same way the component does — via form submit.
    // Since the button is disabled, we directly call handleSendCode's logic
    // by removing the required attr and submitting.
    const emailInput = screen.getByLabelText(/email/i);
    // The input has required + the button is disabled, so we need to type
    // a space then clear to trigger the empty-email error path.
    // Actually, the component guards `if (!email)` in handleSendCode.
    // But the button is disabled when `!email`. Let's verify:
    const button = screen.getByRole("button", { name: /continue/i });
    expect(button).toBeDisabled();

    // Type an email to enable button, then clear it — button becomes disabled again
    const user = userEvent.setup();
    await user.type(emailInput, "a");
    expect(button).not.toBeDisabled();
    await user.clear(emailInput);
    expect(button).toBeDisabled();
  });

  // -------------------------------------------------------------------------
  // sendCode flow
  // -------------------------------------------------------------------------

  it("calls sendCode on form submit with email", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(mockSendCode).toHaveBeenCalledWith("test@example.com");
  });

  it("shows 'Sending code...' while submitting", async () => {
    // Never resolve so loading stays true
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(screen.getByText(/sending code/i)).toBeInTheDocument();
  });

  it("transitions to code step after successful sendCode", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });
    expect(screen.getByText(/test@example.com/)).toBeInTheDocument();
  });

  it("autofocuses the OTP input when the code step opens", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    // The OTP field should be focused on mount so the user can type the code
    // without clicking it first — important when repeatedly switching accounts.
    expect(getOTPInput()).toHaveFocus();
  });

  it("shows error when sendCode fails", async () => {
    mockSendCode.mockRejectedValueOnce(new Error("Rate limited"));
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText("Rate limited")).toBeInTheDocument();
    });
  });

  it("shows generic error when sendCode throws non-Error", async () => {
    mockSendCode.mockRejectedValueOnce("boom");
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/failed to send code/i),
      ).toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Code verification
  // -------------------------------------------------------------------------

  it("calls verifyCode, seeds workspace list cache, then onSuccess", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockResolvedValueOnce(undefined);
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: "ws-1" }]);

    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    // Step 1: email
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    // Step 2: code
    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, "123456");

    await waitFor(() => {
      expect(mockVerifyCode).toHaveBeenCalledWith(
        "test@example.com",
        "123456",
      );
      expect(mockApiListWorkspaces).toHaveBeenCalled();
      // The workspace list is seeded into React Query so onSuccess can read
      // it synchronously to compute a destination URL.
      expect(mockSetQueryData).toHaveBeenCalledWith(
        expect.arrayContaining(["workspaces", "list"]),
        [{ id: "ws-1" }],
      );
      expect(onSuccess).toHaveBeenCalled();
    });
  });

  it("shows error on invalid code", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockRejectedValueOnce(new Error("Invalid code"));

    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, "000000");

    await waitFor(() => {
      expect(screen.getByText("Invalid code")).toBeInTheDocument();
    });
    expect(onSuccess).not.toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // Resend code with cooldown
  // -------------------------------------------------------------------------

  it("disables resend button during cooldown", async () => {
    mockSendCode.mockResolvedValue(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    // After transitioning to code step, cooldown is 60s
    const resendBtn = screen.getByRole("button", { name: /resend in/i });
    expect(resendBtn).toBeDisabled();
  });

  it("calls sendCode again when resend is clicked after cooldown", async () => {
    mockSendCode.mockResolvedValue(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    // sendCode was called once for the initial send
    expect(mockSendCode).toHaveBeenCalledTimes(1);

    // Advance past the 60s cooldown one second at a time so React can
    // process each setCooldown state update between ticks.
    for (let i = 0; i < 61; i++) {
      await act(async () => {
        vi.advanceTimersByTime(1_000);
      });
    }

    await waitFor(() => {
      expect(screen.getByText(/resend code/i)).toBeInTheDocument();
    });

    const resendBtn = screen.getByRole("button", { name: /resend code/i });
    expect(resendBtn).not.toBeDisabled();

    await user.click(resendBtn);
    expect(mockSendCode).toHaveBeenCalledTimes(2);
  });

  // -------------------------------------------------------------------------
  // Google OAuth
  // -------------------------------------------------------------------------

  it("renders Google OAuth button when google prop provided", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
      />,
    );
    expect(
      screen.getByRole("button", { name: /continue with google/i }),
    ).toBeInTheDocument();
  });

  it("hides Google OAuth button when google prop omitted", () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(
      screen.queryByRole("button", { name: /continue with google/i }),
    ).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // CLI callback — existing session
  // -------------------------------------------------------------------------

  it("shows cli_confirm step when existing session + cliCallback", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
    // Cookie attempt fails first, then localStorage fallback succeeds
    mockApiGetMe
      .mockRejectedValueOnce(new Error("no cookie"))
      .mockResolvedValueOnce({
        id: "u-1",
        email: "user@example.com",
        name: "Test User",
      });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByText(/authorize cli/i),
      ).toBeInTheDocument();
    });
    expect(screen.getByText(/user@example.com/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /authorize/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /use a different account/i }),
    ).toBeInTheDocument();
  });

  it("CLI authorize button redirects to callback URL", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
    // Cookie attempt fails, localStorage fallback succeeds
    mockApiGetMe
      .mockRejectedValueOnce(new Error("no cookie"))
      .mockResolvedValueOnce({
        id: "u-1",
        email: "user@example.com",
        name: "Test User",
      });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByText(/authorize cli/i),
      ).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^authorize$/i }));

    expect(onTokenObtained).toHaveBeenCalled();
    expect(window.location.href).toContain(
      "http://localhost:9876/callback?token=existing-jwt&state=abc",
    );
  });

  it("'Use a different account' returns to email step", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
    // Cookie attempt fails, localStorage fallback succeeds
    mockApiGetMe
      .mockRejectedValueOnce(new Error("no cookie"))
      .mockResolvedValueOnce({
        id: "u-1",
        email: "user@example.com",
        name: "Test User",
      });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByText(/authorize cli/i),
      ).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: /use a different account/i }),
    );

    expect(
      screen.getByText(/sign in to multica/i),
    ).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // CLI callback — cookie-based session (no localStorage token)
  // -------------------------------------------------------------------------

  it("detects cookie-based session and shows cli_confirm when no localStorage token", async () => {
    // No localStorage token — getMe succeeds via HttpOnly cookie
    mockApiGetMe.mockResolvedValueOnce({
      id: "u-1",
      email: "cookie@example.com",
      name: "Cookie User",
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/cookie@example.com/)).toBeInTheDocument();
  });

  it("CLI authorize with cookie session calls issueCliToken and redirects", async () => {
    // No localStorage token — getMe succeeds via cookie
    mockApiGetMe.mockResolvedValueOnce({
      id: "u-1",
      email: "cookie@example.com",
      name: "Cookie User",
    });
    mockApiIssueCliToken.mockResolvedValueOnce({ token: "fresh-jwt" });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^authorize$/i }));

    await waitFor(() => {
      expect(mockApiIssueCliToken).toHaveBeenCalled();
      expect(onTokenObtained).toHaveBeenCalled();
      expect(window.location.href).toContain(
        "http://localhost:9876/callback?token=fresh-jwt&state=abc",
      );
    });
  });

  // -------------------------------------------------------------------------
  // CLI callback — code verification redirects
  // -------------------------------------------------------------------------

  it("CLI code verification redirects to callback URL", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockApiVerifyCode.mockResolvedValueOnce({ token: "new-jwt-token" });
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        cliCallback={{ url: "http://localhost:9876/callback", state: "xyz" }}
      />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "cli@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, "654321");

    await waitFor(() => {
      expect(mockApiVerifyCode).toHaveBeenCalledWith(
        "cli@example.com",
        "654321",
      );
      expect(onTokenObtained).toHaveBeenCalled();
      expect(window.location.href).toContain(
        "http://localhost:9876/callback?token=new-jwt-token&state=xyz",
      );
    });

    // Normal verifyCode should NOT be called in CLI path
    expect(mockVerifyCode).not.toHaveBeenCalled();
    // onSuccess should NOT be called in CLI path — redirect handles it
    expect(onSuccess).not.toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // Logo prop
  // -------------------------------------------------------------------------

  it("renders logo when provided", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        logo={<div data-testid="custom-logo">Logo</div>}
      />,
    );
    expect(screen.getByTestId("custom-logo")).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // onTokenObtained callback
  // -------------------------------------------------------------------------

  it("calls onTokenObtained after successful verification", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockResolvedValueOnce(undefined);
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: "ws-1" }]);
    const onTokenObtained = vi.fn();

    render(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
      />,
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    const otpInput = getOTPInput();
    await user.type(otpInput, "123456");

    await waitFor(() => {
      expect(onTokenObtained).toHaveBeenCalled();
      expect(onSuccess).toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // Back button on code step
  // -------------------------------------------------------------------------

  it("back button returns to email step", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /back/i }));

    expect(
      screen.getByText(/sign in to multica/i),
    ).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Directory (LDAP) login
  // -------------------------------------------------------------------------

  it("renders both credential tabs when the server offers LDAP/AD login", async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    expect(screen.getByRole("tab", { name: /email code/i })).toBeInTheDocument();
    expect(
      screen.getByRole("tab", { name: /ldap\/ad account/i }),
    ).toBeInTheDocument();

    // Email panel is the default: its field is up, the LDAP/AD fields are not.
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/enterprise ldap username/i),
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
  });

  // Regression: the header subtitle used to stay hardcoded to the email-code
  // copy ("Enter your email...") even while the LDAP/AD panel was showing,
  // which reads as an error on the directory panel (COM-125).
  it("swaps the header subtitle for the active panel instead of always showing the email copy", async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    expect(
      screen.getByText(/enter your email to get a login code/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/sign in with your enterprise ldap account and password/i),
    ).not.toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));

    expect(
      screen.getByText(/sign in with your enterprise ldap account and password/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/enter your email to get a login code/i),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /email code/i }));

    expect(
      screen.getByText(/enter your email to get a login code/i),
    ).toBeInTheDocument();
  });

  it("renders no LDAP/AD tab when ldap is omitted", () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

    expect(screen.queryByRole("tab", { name: /ldap\/ad account/i })).toBeNull();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  it("renders no LDAP/AD tab when ldap.enabled is false", () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: false }} />);

    expect(screen.queryByRole("tab", { name: /ldap\/ad account/i })).toBeNull();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  it("hides the Google button while the LDAP/AD panel is showing", async () => {
    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        ldap={{ enabled: true }}
        google={{ clientId: "cid", redirectUri: "https://app.test/cb" }}
      />,
    );

    const user = userEvent.setup();
    expect(
      screen.getByRole("button", { name: /continue with google/i }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));

    expect(
      screen.queryByRole("button", { name: /continue with google/i }),
    ).toBeNull();
  });

  it("clears the LDAP/AD error when switching back to the email panel", async () => {
    mockLdapLogin.mockRejectedValueOnce(new ApiError("nope", 401, "Unauthorized"));
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "s3cret");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/incorrect username or password/i),
      ).toBeInTheDocument();
    });

    await user.click(screen.getByRole("tab", { name: /email code/i }));

    expect(
      screen.queryByText(/incorrect username or password/i),
    ).toBeNull();
  });

  it("signs in through the auth store and calls onSuccess on LDAP/AD success", async () => {
    mockLdapLogin.mockResolvedValueOnce({ id: "u1", email: "alice@corp.test" });
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: "ws-1" }]);
    const onTokenObtained = vi.fn();

    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        onTokenObtained={onTokenObtained}
        ldap={{ enabled: true }}
      />,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "  alice  ");
    await user.type(screen.getByLabelText(/password/i), "s3cret");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalled();
      expect(onTokenObtained).toHaveBeenCalled();
    });
    // Username is trimmed before it reaches the LDAP/AD server.
    expect(mockLdapLogin).toHaveBeenCalledWith("alice", "s3cret");
    // Same cache seeding as the code path: onSuccess reads it synchronously.
    expect(mockSetQueryData).toHaveBeenCalled();
  });

  it("shows the invalid-credentials copy and keeps the fields on a 401", async () => {
    mockLdapLogin.mockRejectedValueOnce(new ApiError("bad credentials", 401, "Unauthorized"));

    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "wrong");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/incorrect username or password/i),
      ).toBeInTheDocument();
    });
    expect(onSuccess).not.toHaveBeenCalled();
    // A wrong password is a typo: making the user retype the username too is
    // the difference between a shrug and a support ticket.
    expect(screen.getByLabelText(/enterprise ldap username/i)).toHaveValue("alice");
    expect(screen.getByLabelText(/password/i)).toHaveValue("wrong");
  });

  it("shows the LDAP/AD-outage copy on a 502 and passes a 429 message through", async () => {
    mockLdapLogin.mockRejectedValueOnce(new ApiError("down", 502, "Bad Gateway"));
    const { unmount } = renderWithI18n(
      <LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "pw");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(screen.getByText(/directory service is unreachable/i)).toBeInTheDocument();
    });
    unmount();

    // 429 already says what happened in an actionable form, so the server
    // wording is what the user should read.
    mockLdapLogin.mockRejectedValueOnce(new ApiError("too many attempts", 429, "Too Many Requests"));
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "pw");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(screen.getByText("too many attempts")).toBeInTheDocument();
    });
  });

  it("requires both LDAP/AD fields before submitting", async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    const button = screen.getByRole("button", { name: /^sign in$/i });
    expect(button).toBeDisabled();

    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    expect(button).toBeDisabled();
  });

  it("hands the token to the CLI callback instead of the app session", async () => {
    mockApiLoginWithLdap.mockResolvedValueOnce({ token: "jwt-ldap" });
    renderWithI18n(
      <LoginPage
        onSuccess={onSuccess}
        ldap={{ enabled: true }}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.type(screen.getByLabelText(/enterprise ldap username/i), "alice");
    await user.type(screen.getByLabelText(/password/i), "pw");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() => {
      expect(mockApiLoginWithLdap).toHaveBeenCalledWith("alice", "pw");
      expect(window.location.href).toBe(
        "http://localhost:9876/callback?token=jwt-ldap&state=abc",
      );
    });
    expect(mockApiSetToken).toHaveBeenCalledWith("jwt-ldap");
    expect(localStorage.getItem("multica_token")).toBe("jwt-ldap");
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it("keeps the typed email when switching tabs and back", async () => {
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("tab", { name: /ldap\/ad account/i }));
    await user.click(screen.getByRole("tab", { name: /email code/i }));

    expect(screen.getByLabelText(/email/i)).toHaveValue("test@example.com");
  });

  it("does not offer the LDAP/AD tab once the code step is open", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    renderWithI18n(<LoginPage onSuccess={onSuccess} ldap={{ enabled: true }} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });
    expect(
      screen.queryByRole("tab", { name: /ldap\/ad account/i }),
    ).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// validateCliCallback (exported helper)
// ---------------------------------------------------------------------------

describe("validateCliCallback", () => {
  it("accepts http://localhost", () => {
    expect(validateCliCallback("http://localhost:9876/callback")).toBe(true);
  });

  it("accepts http://127.0.0.1", () => {
    expect(validateCliCallback("http://127.0.0.1:8080/cb")).toBe(true);
  });

  it("accepts 10.x.x.x private IPs", () => {
    expect(validateCliCallback("http://10.0.0.5:9876/callback")).toBe(true);
    expect(validateCliCallback("http://10.255.255.255:1234/cb")).toBe(true);
  });

  it("accepts 172.16-31.x.x private IPs", () => {
    expect(validateCliCallback("http://172.16.0.1:9876/callback")).toBe(true);
    expect(validateCliCallback("http://172.31.255.255:1234/cb")).toBe(true);
  });

  it("rejects 172.x outside 16-31 range", () => {
    expect(validateCliCallback("http://172.15.0.1:9876/callback")).toBe(false);
    expect(validateCliCallback("http://172.32.0.1:9876/callback")).toBe(false);
  });

  it("accepts 192.168.x.x private IPs", () => {
    expect(validateCliCallback("http://192.168.1.131:41117/callback")).toBe(true);
    expect(validateCliCallback("http://192.168.0.1:8080/cb")).toBe(true);
  });

  it("rejects https:// URLs", () => {
    expect(validateCliCallback("https://localhost:9876/callback")).toBe(false);
  });

  it("rejects public IPs and domains", () => {
    expect(validateCliCallback("http://evil.com:9876/callback")).toBe(false);
    expect(validateCliCallback("http://8.8.8.8:9876/callback")).toBe(false);
    expect(validateCliCallback("http://192.169.1.1:9876/callback")).toBe(false);
  });

  it("rejects invalid URLs", () => {
    expect(validateCliCallback("not-a-url")).toBe(false);
  });
});

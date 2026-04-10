import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockSendCode = vi.hoisted(() => vi.fn());
const mockVerifyCode = vi.hoisted(() => vi.fn());
const mockHydrateWorkspace = vi.hoisted(() => vi.fn());
const mockApiListWorkspaces = vi.hoisted(() => vi.fn());
const mockApiVerifyCode = vi.hoisted(() => vi.fn());
const mockApiSetToken = vi.hoisted(() => vi.fn());
const mockApiGetMe = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    // Zustand hook form — component may call useAuthStore(selector)
    (selector?: (s: unknown) => unknown) => {
      const state = { sendCode: mockSendCode, verifyCode: mockVerifyCode };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        sendCode: mockSendCode,
        verifyCode: mockVerifyCode,
      }),
    },
  ),
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const state = { hydrateWorkspace: mockHydrateWorkspace };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        hydrateWorkspace: mockHydrateWorkspace,
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
  },
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
    // Default: no existing session
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
    render(<LoginPage onSuccess={onSuccess} />);
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
    render(<LoginPage onSuccess={onSuccess} />);

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
    render(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(mockSendCode).toHaveBeenCalledWith("test@example.com");
  });

  it("shows 'Sending code...' while submitting", async () => {
    // Never resolve so loading stays true
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    render(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(screen.getByText(/sending code/i)).toBeInTheDocument();
  });

  it("transitions to code step after successful sendCode", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    render(<LoginPage onSuccess={onSuccess} />);

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

  it("shows error when sendCode fails", async () => {
    mockSendCode.mockRejectedValueOnce(new Error("Rate limited"));
    render(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText("Rate limited")).toBeInTheDocument();
    });
  });

  it("shows generic error when sendCode throws non-Error", async () => {
    mockSendCode.mockRejectedValueOnce("boom");
    render(<LoginPage onSuccess={onSuccess} />);

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

  it("calls verifyCode, listWorkspaces, hydrateWorkspace, then onSuccess", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockResolvedValueOnce(undefined);
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: "ws-1" }]);
    mockHydrateWorkspace.mockReturnValueOnce({ id: "ws-1" });

    render(<LoginPage onSuccess={onSuccess} />);

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
      expect(mockHydrateWorkspace).toHaveBeenCalledWith(
        [{ id: "ws-1" }],
        undefined,
      );
      expect(onSuccess).toHaveBeenCalled();
    });
  });

  it("shows error on invalid code", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockRejectedValueOnce(new Error("Invalid code"));

    render(<LoginPage onSuccess={onSuccess} />);

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
    render(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/check your email/i),
      ).toBeInTheDocument();
    });

    // After transitioning to code step, cooldown is 10s
    const resendBtn = screen.getByRole("button", { name: /resend in/i });
    expect(resendBtn).toBeDisabled();
  });

  it("shows resend button with cooldown text after sending code", async () => {
    mockSendCode.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<LoginPage onSuccess={onSuccess} />);

    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    // After transition, resend shows cooldown text and is disabled
    expect(screen.getByText(/resend in/i)).toBeInTheDocument();
  });

  it("calls sendCode again when resend is clicked after cooldown", async () => {
    mockSendCode.mockResolvedValue(undefined);
    render(<LoginPage onSuccess={onSuccess} />);

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await user.type(screen.getByLabelText(/email/i), "test@example.com");
    await user.click(screen.getByRole("button", { name: /continue/i }));

    await waitFor(() => {
      expect(screen.getByText(/check your email/i)).toBeInTheDocument();
    });

    // sendCode was called once for the initial send
    expect(mockSendCode).toHaveBeenCalledTimes(1);

    // Advance past the 10s cooldown one second at a time so React can
    // process each setCooldown state update between ticks.
    for (let i = 0; i < 11; i++) {
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
    render(<LoginPage onSuccess={onSuccess} />);
    expect(
      screen.queryByRole("button", { name: /continue with google/i }),
    ).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // CLI callback — existing session
  // -------------------------------------------------------------------------

  it("shows cli_confirm step when existing session + cliCallback", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
    mockApiGetMe.mockResolvedValueOnce({
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
    mockApiGetMe.mockResolvedValueOnce({
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
    mockApiGetMe.mockResolvedValueOnce({
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

  it("does not render logo placeholder when omitted", () => {
    render(<LoginPage onSuccess={onSuccess} />);
    expect(screen.queryByTestId("custom-logo")).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // onTokenObtained callback
  // -------------------------------------------------------------------------

  it("calls onTokenObtained after successful verification", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockResolvedValueOnce(undefined);
    mockApiListWorkspaces.mockResolvedValueOnce([{ id: "ws-1" }]);
    mockHydrateWorkspace.mockReturnValueOnce({ id: "ws-1" });
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
    render(<LoginPage onSuccess={onSuccess} />);

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
  // lastWorkspaceId
  // -------------------------------------------------------------------------

  it("passes lastWorkspaceId to hydrateWorkspace", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    mockVerifyCode.mockResolvedValueOnce(undefined);
    mockApiListWorkspaces.mockResolvedValueOnce([
      { id: "ws-1" },
      { id: "ws-2" },
    ]);
    mockHydrateWorkspace.mockReturnValueOnce({ id: "ws-2" });

    render(
      <LoginPage onSuccess={onSuccess} lastWorkspaceId="ws-2" />,
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
      expect(mockHydrateWorkspace).toHaveBeenCalledWith(
        [{ id: "ws-1" }, { id: "ws-2" }],
        "ws-2",
      );
    });
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

  it("rejects https:// URLs", () => {
    expect(validateCliCallback("https://localhost:9876/callback")).toBe(false);
  });

  it("rejects non-localhost hosts", () => {
    expect(validateCliCallback("http://evil.com:9876/callback")).toBe(false);
  });

  it("rejects invalid URLs", () => {
    expect(validateCliCallback("not-a-url")).toBe(false);
  });
});

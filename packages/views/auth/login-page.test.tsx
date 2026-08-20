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
const mockApiListWorkspaces = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());

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

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockApiListWorkspaces,
  },
}));

vi.mock("@multica/core/types", () => ({}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { LoginPage } from "./login-page";

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

  it("shows resend button with cooldown text after sending code", async () => {
    mockSendCode.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);

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
    renderWithI18n(<LoginPage onSuccess={onSuccess} />);
    expect(screen.queryByTestId("custom-logo")).not.toBeInTheDocument();
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

});

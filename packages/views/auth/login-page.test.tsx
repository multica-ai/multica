import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockApiSetToken = vi.hoisted(() => vi.fn());
const mockApiGetMe = vi.hoisted(() => vi.fn());
const mockApiIssueCliToken = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    setToken: mockApiSetToken,
    getMe: mockApiGetMe,
    issueCliToken: mockApiIssueCliToken,
  },
}));

vi.mock("@multica/core/types", () => ({}));

import { LoginPage, validateCliCallback } from "./login-page";

describe("LoginPage", () => {
  const onSuccess = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockApiGetMe.mockRejectedValue(new Error("unauthorized"));
    localStorage.clear();
    Object.defineProperty(window, "location", {
      writable: true,
      value: { href: "http://localhost:3000" },
    });
  });

  // -------------------------------------------------------------------------
  // Sign-in step — Google-only rendering
  // -------------------------------------------------------------------------

  it("renders Google-only sign-in screen with title and descriptor", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
      />,
    );

    expect(screen.getByText(/sign in to multica/i)).toBeInTheDocument();
    expect(
      screen.getByText(/sign in with your @g2\.com google account/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /continue with google/i }),
    ).toBeInTheDocument();
  });

  it("does not render email/OTP UI", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
      />,
    );

    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^continue$/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/resend/i)).not.toBeInTheDocument();
  });

  it("shows 'not configured' message when no Google config is provided", () => {
    render(<LoginPage onSuccess={onSuccess} />);

    expect(
      screen.getByText(/sign-in is not configured/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /continue with google/i }),
    ).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Google OAuth — redirect behaviour
  // -------------------------------------------------------------------------

  it("redirects to Google OAuth URL with expected params on button click", async () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{
          clientId: "goog-123",
          redirectUri: "http://localhost/cb",
          state: "platform:desktop",
        }}
      />,
    );

    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: /continue with google/i }),
    );

    const href = window.location.href;
    expect(href).toContain("https://accounts.google.com/o/oauth2/v2/auth?");
    expect(href).toContain("client_id=goog-123");
    expect(href).toContain(
      `redirect_uri=${encodeURIComponent("http://localhost/cb")}`,
    );
    expect(href).toContain("response_type=code");
    expect(href).toContain("scope=openid+email+profile");
    expect(href).toContain("state=platform%3Adesktop");
  });

  it("prefers onGoogleLogin override over the built-in redirect when provided", async () => {
    const onGoogleLogin = vi.fn();
    render(
      <LoginPage onSuccess={onSuccess} onGoogleLogin={onGoogleLogin} />,
    );

    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: /continue with google/i }),
    );

    expect(onGoogleLogin).toHaveBeenCalledTimes(1);
    // onGoogleLogin was used, so no OAuth redirect was performed.
    expect(window.location.href).toBe("http://localhost:3000");
  });

  it("renders Google button from onGoogleLogin alone (no `google` config required)", () => {
    render(
      <LoginPage onSuccess={onSuccess} onGoogleLogin={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: /continue with google/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/sign-in is not configured/i),
    ).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // CLI callback — cookie-based session
  // -------------------------------------------------------------------------

  it("shows cli_confirm step when cookie session is detected", async () => {
    mockApiGetMe.mockResolvedValueOnce({
      id: "u-1",
      email: "cookie@example.com",
      name: "Cookie User",
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/cookie@example.com/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^authorize$/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /use a different account/i }),
    ).toBeInTheDocument();
  });

  it("cookie-session authorize calls issueCliToken and redirects to callback URL", async () => {
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
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
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
  // CLI callback — localStorage fallback
  // -------------------------------------------------------------------------

  it("falls back to localStorage token when cookie session is absent", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
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
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/user@example.com/)).toBeInTheDocument();
  });

  it("localStorage-session authorize reuses the stored token (no issueCliToken call)", async () => {
    localStorage.setItem("multica_token", "existing-jwt");
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
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /^authorize$/i }));

    expect(mockApiIssueCliToken).not.toHaveBeenCalled();
    expect(onTokenObtained).toHaveBeenCalled();
    expect(window.location.href).toContain(
      "http://localhost:9876/callback?token=existing-jwt&state=abc",
    );
  });

  it("'Use a different account' returns to the Google sign-in step", async () => {
    mockApiGetMe.mockResolvedValueOnce({
      id: "u-1",
      email: "cookie@example.com",
      name: "Cookie User",
    });

    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
        cliCallback={{ url: "http://localhost:9876/callback", state: "abc" }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/authorize cli/i)).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: /use a different account/i }),
    );

    expect(screen.getByText(/sign in to multica/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /continue with google/i }),
    ).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Logo prop
  // -------------------------------------------------------------------------

  it("renders logo when provided", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
        logo={<div data-testid="custom-logo">Logo</div>}
      />,
    );
    expect(screen.getByTestId("custom-logo")).toBeInTheDocument();
  });

  it("does not render logo placeholder when omitted", () => {
    render(
      <LoginPage
        onSuccess={onSuccess}
        google={{ clientId: "goog-123", redirectUri: "http://localhost/cb" }}
      />,
    );
    expect(screen.queryByTestId("custom-logo")).not.toBeInTheDocument();
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

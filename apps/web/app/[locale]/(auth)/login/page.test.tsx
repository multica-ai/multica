import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import enMessages from "@/messages/en.json";

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/login",
  useSearchParams: () => new URLSearchParams(),
}));

// Mock auth store
const mockSendCode = vi.fn();
const mockVerifyCode = vi.fn();
vi.mock("@/platform/auth", () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({
      sendCode: mockSendCode,
      verifyCode: mockVerifyCode,
      user: null,
      isLoading: false,
    }),
}));

// Mock auth-cookie
vi.mock("@/features/auth/auth-cookie", () => ({
  setLoggedInCookie: vi.fn(),
}));

// Mock workspace store
const mockHydrateWorkspace = vi.fn();
vi.mock("@/platform/workspace", () => ({
  useWorkspaceStore: (selector: (s: any) => any) =>
    selector({
      hydrateWorkspace: mockHydrateWorkspace,
    }),
}));

// Mock api
vi.mock("@/platform/api", () => ({
  api: {
    listWorkspaces: vi.fn().mockResolvedValue([]),
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
  },
}));

import LoginPage from "./page";

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <NextIntlClientProvider locale="en" messages={enMessages}>
      {ui}
    </NextIntlClientProvider>
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders login form with email input and send code button", () => {
    renderWithProviders(<LoginPage />);

    expect(screen.getByText("Multica")).toBeInTheDocument();
    expect(screen.getByText("Enter your email to receive a verification code")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Send code" })
    ).toBeInTheDocument();
  });

  it("does not call sendCode when email is empty", async () => {
    const user = userEvent.setup();
    renderWithProviders(<LoginPage />);

    await user.click(screen.getByRole("button", { name: "Send code" }));
    expect(mockSendCode).not.toHaveBeenCalled();
  });

  it("calls sendCode with email on submit", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    renderWithProviders(<LoginPage />);

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Send code" }));

    await waitFor(() => {
      expect(mockSendCode).toHaveBeenCalledWith("test@multica.ai");
    });
  });

  it("shows 'Sending...' while submitting", async () => {
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    const user = userEvent.setup();
    renderWithProviders(<LoginPage />);

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Send code" }));

    await waitFor(() => {
      expect(screen.getByText("Sending...")).toBeInTheDocument();
    });
  });

  it("shows verification code step after sending code", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    renderWithProviders(<LoginPage />);

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Send code" }));

    await waitFor(() => {
      expect(screen.getByText("Verification code")).toBeInTheDocument();
    });
  });

  it("shows error when sendCode fails", async () => {
    mockSendCode.mockRejectedValueOnce(new Error("Network error"));
    const user = userEvent.setup();
    renderWithProviders(<LoginPage />);

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Send code" }));

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
  });
});

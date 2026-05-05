import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

const navigate = vi.fn();
const logout = vi.fn();

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate, replace: navigate }),
}));

vi.mock("../auth", () => ({
  useLogout: () => logout,
}));

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

import { NoAccessPage } from "./no-access-page";

describe("NoAccessPage", () => {
  beforeEach(() => {
    navigate.mockReset();
    logout.mockReset();
  });

  it("renders generic message that doesn't leak existence", () => {
    render(<NoAccessPage />);
    expect(
      screen.getByText(/doesn't exist or you don't have access/i),
    ).toBeInTheDocument();
  });

  it("navigates to root on 'Go to my workspaces'", () => {
    render(<NoAccessPage />);
    fireEvent.click(screen.getByRole("button", { name: /go to my workspaces/i }));
    expect(navigate).toHaveBeenCalledWith("/");
  });

  it("fully logs out on 'Sign in as a different user' instead of just navigating", () => {
    render(<NoAccessPage />);
    fireEvent.click(
      screen.getByRole("button", { name: /sign in as a different user/i }),
    );
    expect(logout).toHaveBeenCalledTimes(1);
    // Should NOT just navigate to /login — that would leave the session
    // cookie + auth state intact and AuthInitializer would re-auth.
    expect(navigate).not.toHaveBeenCalledWith("/login");
  });
});

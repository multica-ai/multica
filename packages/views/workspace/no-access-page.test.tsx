import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enWorkspace from "../locales/en/workspace.json";
import { NoAccessPage } from "./no-access-page";

const TEST_RESOURCES = {
  en: { common: enCommon, workspace: enWorkspace },
};

const navigate = vi.fn();
const logout = vi.fn();

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate, replace: navigate }),
}));

vi.mock("../auth", () => ({
  useLogout: () => logout,
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage() {
  return render(<NoAccessPage />, { wrapper: I18nWrapper });
}

describe("NoAccessPage", () => {
  beforeEach(() => {
    navigate.mockReset();
    logout.mockReset();
  });

  it("renders generic message that doesn't leak existence", () => {
    renderPage();
    expect(
      screen.getByText(/doesn't exist or you don't have access/i),
    ).toBeInTheDocument();
  });

  it("navigates to root on 'Go to my workspaces'", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: /go to my workspaces/i }));
    expect(navigate).toHaveBeenCalledWith("/");
  });

  it("clears last_workspace_slug cookie on mount so the proxy stops looping us back", () => {
    document.cookie = "last_workspace_slug=stale; path=/";
    render(<NoAccessPage />);
    // Assert empty value, not just absence of "stale" — the proxy reads any
    // truthy value as a redirect target, so a buggy clear that left e.g.
    // `last_workspace_slug=other` would still trap users.
    const value = document.cookie.match(/last_workspace_slug=([^;]*)/)?.[1];
    expect(value ?? "").toBe("");
  });

  it("fully logs out on 'Sign in as a different user' instead of just navigating", () => {
    renderPage();
    fireEvent.click(
      screen.getByRole("button", { name: /sign in as a different user/i }),
    );
    expect(logout).toHaveBeenCalledTimes(1);
    // Should NOT just navigate to /login — that would leave the session
    // cookie + auth state intact and AuthInitializer would re-auth.
    expect(navigate).not.toHaveBeenCalledWith("/login");
  });
});

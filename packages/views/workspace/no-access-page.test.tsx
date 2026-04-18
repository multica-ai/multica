import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { NoAccessPage } from "./no-access-page";

const navigate = vi.fn();
const logout = vi.fn();

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate, replace: navigate }),
}));

vi.mock("../auth", () => ({
  useLogout: () => logout,
}));

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

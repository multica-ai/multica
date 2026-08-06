import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { OnboardingLogoutButton } from "./onboarding-logout-button";

const { logout } = vi.hoisted(() => ({ logout: vi.fn() }));

vi.mock("../../auth", () => ({
  useLogout: () => logout,
}));

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("OnboardingLogoutButton", () => {
  it("logs out from onboarding", async () => {
    const user = userEvent.setup();
    render(<OnboardingLogoutButton />, { wrapper: I18nWrapper });

    const button = screen.getByRole("button", { name: "Log out" });
    expect(button).toHaveClass("right-8", "top-8");

    await user.click(button);

    expect(logout).toHaveBeenCalledOnce();
  });
});

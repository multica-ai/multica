import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { useAppearanceStore } from "@multica/core/appearance";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const themeState = vi.hoisted(() => ({
  theme: "light",
  resolvedTheme: "light",
  setTheme: vi.fn(),
}));
vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => themeState,
}));

import { AppearanceTab } from "./appearance-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const group = (name: string) => screen.getByRole("group", { name });

describe("AppearanceTab", () => {
  beforeEach(() => {
    themeState.theme = "light";
    themeState.resolvedTheme = "light";
    themeState.setTheme.mockClear();
    useAppearanceStore.setState({ style: "default" });
  });
  afterEach(() => {
    cleanup();
  });

  it("applies the theme on this device", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    expect(screen.getByText("This device only")).toBeInTheDocument();
    expect(within(group("Theme")).getByRole("button", { name: "Light" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(within(group("Theme")).getByRole("button", { name: "Dark" }));

    expect(themeState.setTheme).toHaveBeenCalledWith("dark");
  });

  it("previews each style in the applied mode and saves the pick", async () => {
    themeState.theme = "system";
    themeState.resolvedTheme = "dark";
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    const pop = within(group("Style")).getByRole("button", { name: "Pop" });
    expect(pop).toHaveAccessibleDescription(enSettings.appearance.style.pop_description);
    expect(pop.querySelector("[data-appearance-preview]")).toHaveAttribute(
      "data-preview-mode",
      "dark",
    );

    await user.click(pop);

    expect(useAppearanceStore.getState().style).toBe("pop");
    expect(pop).toHaveAttribute("aria-pressed", "true");
  });

  it("previews each theme with the current style", () => {
    useAppearanceStore.setState({ style: "pop" });
    render(<AppearanceTab />, { wrapper: Wrapper });

    const dark = within(group("Theme")).getByRole("button", { name: "Dark" });
    expect(dark.querySelector("[data-appearance-preview]")).toHaveAttribute(
      "data-preview-style",
      "pop",
    );
  });
});

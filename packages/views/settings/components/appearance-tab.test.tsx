import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { DEFAULT_APPEARANCE, useAppearanceStore } from "@multica/core/appearance";
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
const state = () => useAppearanceStore.getState();

describe("AppearanceTab", () => {
  beforeEach(() => {
    themeState.theme = "light";
    themeState.resolvedTheme = "light";
    themeState.setTheme.mockClear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
  });
  afterEach(() => {
    cleanup();
  });

  it("switches the mode on this device", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    expect(screen.getByText("This device only")).toBeInTheDocument();
    expect(within(group("Mode")).getByRole("button", { name: "Light" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(within(group("Mode")).getByRole("button", { name: "Dark" }));

    expect(themeState.setTheme).toHaveBeenCalledWith("dark");
  });

  it("previews every theme in the applied mode and saves the pick", async () => {
    themeState.theme = "system";
    themeState.resolvedTheme = "dark";
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    const themes = within(group("Theme")).getAllByRole("button");
    expect(themes.map((b) => b.textContent)).toEqual(["Default", "Pop", "GitHub", "Blueprint", "Notion"]);
    const github = within(group("Theme")).getByRole("button", { name: "GitHub" });
    expect(github.querySelector("[data-appearance-preview]")).toHaveAttribute(
      "data-preview-mode",
      "dark",
    );

    await user.click(github);

    expect(state().theme).toBe("github");
    expect(github).toHaveAttribute("aria-pressed", "true");
  });

  it("picks the accent, fonts and density", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("radio", { name: "Violet" }));
    await user.click(within(group("Interface font")).getByRole("button", { name: /Source Serif 4/ }));
    await user.click(within(group("CJK font")).getByRole("button", { name: "Kai" }));
    await user.click(within(group("Density")).getByRole("button", { name: "Compact" }));
    await user.click(screen.getByRole("switch", { name: "Increase contrast" }));

    expect(state()).toMatchObject({
      accent: "violet",
      uiFont: "serif",
      cjkFont: "kai",
      density: "compact",
      highContrast: true,
    });
    expect(screen.getByRole("radio", { name: "Violet" })).toHaveAttribute("aria-checked", "true");
  });

  it("resets to the defaults, and only once something changed", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });
    const reset = screen.getByRole("button", { name: "Reset to defaults" });
    expect(reset).toBeDisabled();

    await user.click(within(group("Density")).getByRole("button", { name: "Comfortable" }));
    expect(reset).toBeEnabled();
    await user.click(reset);

    expect(state().density).toBe("default");
    expect(reset).toBeDisabled();
  });
});

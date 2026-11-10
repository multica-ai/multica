import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { DEFAULT_APPEARANCE, useAppearanceStore, type Theme } from "@multica/core/appearance";
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

/** A theme the next render treats as active, for option types no built-in theme uses. */
const testTheme = vi.hoisted(() => ({ current: null as Theme | null }));
vi.mock("@multica/core/appearance", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/appearance")>();
  return {
    ...actual,
    activeTheme: (prefs: { theme: string }) =>
      testTheme.current && prefs.theme === testTheme.current.id ? testTheme.current : actual.activeTheme(prefs),
  };
});

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
const themeSelect = () => screen.getByRole("combobox", { name: "Theme" });

describe("AppearanceTab", () => {
  beforeEach(() => {
    themeState.theme = "light";
    themeState.resolvedTheme = "light";
    themeState.setTheme.mockClear();
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    testTheme.current = null;
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

  it("lists every theme in a dropdown, drawn in the applied mode, and saves the pick", async () => {
    themeState.theme = "system";
    themeState.resolvedTheme = "dark";
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.click(themeSelect());
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Default", "Pop", "GitHub", "Blueprint", "Notion"]);
    expect(options[2]!.querySelector("[data-theme-swatch]")).toHaveAttribute("data-swatch-mode", "dark");
    await user.click(options[2]!);

    expect(state().theme).toBe("github");
    expect(themeSelect()).toHaveTextContent("GitHub");
    expect(screen.queryByRole("button", { name: /import/i })).not.toBeInTheDocument();
  });

  it("renders the options the active theme declares and saves them per theme", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });
    expect(screen.queryByText("Theme options")).not.toBeInTheDocument();

    await user.click(themeSelect());
    await user.click(await screen.findByRole("option", { name: "Pop" }));
    expect(screen.getByText("Theme options")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Yellow" })).toHaveAttribute("aria-checked", "true");
    await user.click(screen.getByRole("radio", { name: "Mint" }));
    await user.click(screen.getByRole("switch", { name: "Status blocks" }));

    expect(state().themeOptions).toEqual({ pop: { signal: "mint", "status-blocks": false } });
    await user.click(screen.getByRole("button", { name: "Reset options" }));
    expect(state().themeOptions).toEqual({});
  });

  it("sets interface and content type separately", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    const [uiLatin, contentLatin] = screen.getAllByRole("combobox", { name: "Latin font" });
    const [, contentCjk] = screen.getAllByRole("combobox", { name: "CJK font" });
    await user.click(uiLatin!);
    await user.click(await screen.findByRole("option", { name: /^Source Serif 4/ }));
    await user.click(contentLatin!);
    await user.click(await screen.findByRole("option", { name: /^Geist Mono/ }));
    await user.click(contentCjk!);
    await user.click(await screen.findByRole("option", { name: /^Kai/ }));
    await user.click(within(group("Line height")).getByRole("button", { name: "Relaxed" }));

    expect(state()).toMatchObject({
      uiFont: "serif",
      uiCjkFont: "sans",
      contentFont: "mono",
      contentCjkFont: "kai",
      contentLeading: "relaxed",
    });
    expect(contentLatin).toHaveTextContent("Geist Mono");
  });

  it("names the theme's fonts when the default choice keeps them", () => {
    useAppearanceStore.setState({ theme: "github" });
    const { unmount } = render(<AppearanceTab />, { wrapper: Wrapper });
    const [uiLatin] = screen.getAllByRole("combobox", { name: "Latin font" });
    expect(uiLatin).toHaveTextContent("Theme font");
    expect(screen.getByRole("combobox", { name: "Code font" })).toHaveTextContent("Theme font");
    unmount();

    useAppearanceStore.setState({ theme: "default" });
    render(<AppearanceTab />, { wrapper: Wrapper });
    expect(screen.getAllByRole("combobox", { name: "Latin font" })[0]).toHaveTextContent("Inter");
    expect(screen.getByRole("combobox", { name: "Code font" })).toHaveTextContent("Geist Mono");
  });

  it("picks the density and contrast, and has no accent colour of its own", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });
    expect(screen.queryByRole("radiogroup", { name: "Accent color" })).not.toBeInTheDocument();

    await user.click(within(group("Density")).getByRole("button", { name: "Compact" }));
    await user.click(screen.getByRole("switch", { name: "Increase contrast" }));

    expect(state()).toMatchObject({ density: "compact", highContrast: true });
  });

  it("renders every option type a theme declares, grouped under its headings", async () => {
    const user = userEvent.setup();
    testTheme.current = {
      apiVersion: 1,
      id: "paper",
      name: "Paper",
      css: "",
      options: [
            { id: "page", type: "heading", label: "Page", description: "The paper itself." },
            { id: "paper-color", type: "color", label: "Paper", default: { light: "#f6efdf", dark: "#1d1b16" } },
            { id: "page-width", type: "slider", label: "Width", default: 46, min: 30, max: 60, unit: "rem" },
            { id: "type", type: "heading", label: "Type" },
            { id: "h1-font", type: "font", label: "Heading font", default: "Georgia, serif" },
            { id: "leading", type: "number", label: "Leading", default: 1.6, step: 0.1 },
            { id: "rule", type: "variable-select", label: "Rule", default: "1px", choices: [
              { value: "1px", label: "Thin" },
              { value: "2px", label: "Thick" },
        ] },
      ],
    };
    useAppearanceStore.setState({ theme: "paper" });
    render(<AppearanceTab />, { wrapper: Wrapper });

    expect(screen.getByRole("heading", { name: "Page" })).toBeInTheDocument();
    expect(screen.getByText("The paper itself.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Type" })).toBeInTheDocument();
    expect(screen.getByLabelText("Paper (Dark)")).toHaveValue("#1d1b16");
    expect(screen.getByText("46rem")).toBeInTheDocument();

    const font = () => screen.getByRole("textbox", { name: "Heading font" });
    await user.clear(font());
    await user.type(font(), '"Paper Serif", serif{Enter}');
    expect(state().themeOptions.paper?.["h1-font"]).toBe('"Paper Serif", serif');

    await user.clear(font());
    await user.type(font(), "x; color: red{Enter}");
    expect(state().themeOptions.paper?.["h1-font"]).toBe('"Paper Serif", serif');
    expect(font()).toHaveValue('"Paper Serif", serif');

    const leading = screen.getByRole("spinbutton", { name: "Leading" });
    await user.clear(leading);
    await user.type(leading, "1.8");
    await user.tab();
    expect(state().themeOptions.paper?.leading).toBe(1.8);
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

import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  DEFAULT_APPEARANCE,
  useAppearanceStore,
  useInstalledThemesStore,
} from "@multica/core/appearance";
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

const PAPER = {
  apiVersion: 1,
  id: "paper",
  name: "Paper",
  author: "Someone",
  version: "1.0.0",
  tokens: { light: { "page-canvas": "#f6efdf" }, dark: {} },
  css: '[data-mc="content"] p { hyphens: auto; }',
};

function themeFile(content: unknown, name = "paper.json") {
  return new File([typeof content === "string" ? content : JSON.stringify(content)], name, {
    type: "application/json",
  });
}

function fileInput() {
  return document.querySelector<HTMLInputElement>('input[type="file"]')!;
}

describe("AppearanceTab", () => {
  beforeEach(() => {
    themeState.theme = "light";
    themeState.resolvedTheme = "light";
    themeState.setTheme.mockClear();
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [] });
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

  it("renders the options the active theme declares and saves them per theme", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });
    expect(screen.queryByText("Theme options")).not.toBeInTheDocument();

    await user.click(within(group("Theme")).getByRole("button", { name: "Pop" }));
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

  it("picks the accent and density", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("radio", { name: "Violet" }));
    await user.click(within(group("Density")).getByRole("button", { name: "Compact" }));
    await user.click(screen.getByRole("switch", { name: "Increase contrast" }));

    expect(state()).toMatchObject({ accent: "violet", density: "compact", highContrast: true });
    expect(screen.getByRole("radio", { name: "Violet" })).toHaveAttribute("aria-checked", "true");
  });

  it("imports a theme file after confirmation, and removes it again", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.upload(fileInput(), themeFile(PAPER));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Install “Paper”?")).toBeInTheDocument();
    expect(within(dialog).getByText("Colors · Its own CSS")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Install and use" }));

    expect(useInstalledThemesStore.getState().themes.map((t) => t.id)).toEqual(["paper"]);
    expect(state().theme).toBe("paper");
    expect(within(group("Theme")).getByRole("button", { name: "Paper" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await user.click(screen.getByRole("button", { name: "Remove Paper" }));
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
    expect(state().theme).toBe("default");
  });

  it("lists what is wrong with a theme it cannot install", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.upload(
      fileInput(),
      themeFile({ ...PAPER, css: "body { background: url(https://evil.test/x); }" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Couldn't install this theme")).toBeInTheDocument();
    expect(within(dialog).getByText(/css line 1: url\("https:\/\/evil.test\/x"\)/)).toBeInTheDocument();
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
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

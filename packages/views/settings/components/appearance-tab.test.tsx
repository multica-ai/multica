import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  DEFAULT_APPEARANCE,
  readZip,
  themeFromFiles,
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeFolderStatus,
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
const themeSelect = () => screen.getByRole("combobox", { name: "Theme" });

const PAPER = {
  apiVersion: 1 as const,
  id: "paper",
  name: "Paper",
  author: "Someone",
  version: "1.0.0",
};
const PAPER_CSS = '.theme-light { --page-canvas: #f6efdf; }\n[data-mc="content"] p { hyphens: auto; }';

/** The files of a theme folder, as the folder picker returns them. */
function themeFolder(manifest: unknown, css: string) {
  return [
    new File([JSON.stringify(manifest)], "manifest.json", { type: "application/json" }),
    new File([css], "theme.css", { type: "text/css" }),
  ];
}

function folderInput() {
  return document.querySelector<HTMLInputElement>('input[type="file"][webkitdirectory]')!;
}

describe("AppearanceTab", () => {
  beforeEach(() => {
    themeState.theme = "light";
    themeState.resolvedTheme = "light";
    themeState.setTheme.mockClear();
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [], folders: {} });
    useThemeFolderStatus.setState({ path: null, problems: [] });
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

  it("imports a theme file after confirmation, and removes it again", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "More theme actions" }));
    expect(await screen.findByRole("menuitem", { name: "Theme file (.zip)" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /^Remove/ })).toBeNull();
    await user.keyboard("{Escape}");
    const options = [
      { id: "reading", type: "heading", label: "Reading" },
      { id: "hyphens", type: "toggle", label: "Hyphens", default: false },
    ];
    await user.upload(folderInput(), themeFolder({ ...PAPER, options }, PAPER_CSS));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Install “Paper”?")).toBeInTheDocument();
    expect(within(dialog).getByText("Beta")).toBeInTheDocument();
    expect(within(dialog).getByText(/Custom themes are in beta/)).toBeInTheDocument();
    expect(within(dialog).getByText("theme.css · 1 option")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Install and use" }));

    expect(useInstalledThemesStore.getState().themes.map((t) => t.id)).toEqual(["paper"]);
    expect(state().theme).toBe("paper");
    expect(themeSelect()).toHaveTextContent("Paper");
    await user.click(themeSelect());
    expect(await screen.findByText("Installed")).toBeInTheDocument();
    await user.keyboard("{Escape}");

    await user.click(screen.getByRole("button", { name: "More theme actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Remove Paper" }));
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
    expect(state().theme).toBe("default");
  });

  it("duplicates the active theme as a .zip to edit and import, on web", async () => {
    const user = userEvent.setup();
    const createObjectURL = vi.fn((_blob: Blob) => "blob:theme");
    Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    useAppearanceStore.setState({ theme: "github" });
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "More theme actions" }));
    expect(screen.queryByRole("menuitem", { name: "Open themes folder" })).toBeNull();
    await user.click(await screen.findByRole("menuitem", { name: "Duplicate “GitHub” as a new theme" }));

    expect(click).toHaveBeenCalledTimes(1);
    expect((click.mock.contexts[0] as HTMLAnchorElement).download).toBe("github-copy.zip");
    const archive = await readZip(new Uint8Array(await createObjectURL.mock.calls[0]![0].arrayBuffer()), 1024 * 1024);
    const copy = archive.ok ? themeFromFiles(archive.files) : null;
    expect(copy).toMatchObject({ ok: true, theme: { id: "github-copy", name: "GitHub copy" } });
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
    click.mockRestore();
  });

  describe("on Desktop", () => {
    const desktop = {
      readThemeFolder: vi.fn(async () => ({ path: "/data/themes", entries: [] })),
      onThemeFolderChange: vi.fn(() => () => {}),
      openThemeFolder: vi.fn(async () => ({ ok: true })),
      writeThemeFolder: vi.fn(async () => ({ ok: true as const })),
    };
    beforeEach(() => {
      Object.values(desktop).forEach((fn) => fn.mockClear());
      (window as unknown as { desktopAPI: unknown }).desktopAPI = desktop;
    });
    afterEach(() => {
      delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
    });

    it("duplicates into the themes folder, applies the copy and opens its folder", async () => {
      const user = userEvent.setup();
      desktop.writeThemeFolder.mockResolvedValueOnce({ ok: false, reason: "exists" } as never);
      render(<AppearanceTab />, { wrapper: Wrapper });

      await user.click(screen.getByRole("button", { name: "More theme actions" }));
      await user.click(await screen.findByRole("menuitem", { name: "Duplicate “Default” as a new theme" }));

      expect(desktop.writeThemeFolder.mock.calls.map((call) => (call as unknown[])[0])).toEqual([
        "default-copy",
        "default-copy-2",
      ]);
      expect(state().theme).toBe("default-copy-2");
      expect(useInstalledThemesStore.getState().themes.map((t) => t.id)).toEqual(["default-copy-2"]);
      expect(desktop.openThemeFolder).toHaveBeenCalledWith("default-copy-2");
    });

    it("opens the themes folder, lists its themes on their own and shows what fails the checks", async () => {
      const user = userEvent.setup();
      useInstalledThemesStore.setState({
        themes: [{ ...PAPER, css: PAPER_CSS }],
        folders: { paper: "paper" },
      });
      useThemeFolderStatus.setState({
        path: "/data/themes",
        problems: [{ folder: "ink", errors: ["theme.css line 2: @import is not allowed"] }],
      });
      useAppearanceStore.setState({ theme: "paper" });
      render(<AppearanceTab />, { wrapper: Wrapper });

      expect(screen.getByText(/Some themes in the themes folder don't pass the checks/)).toBeInTheDocument();
      expect(screen.getByText("theme.css line 2: @import is not allowed")).toBeInTheDocument();

      await user.click(themeSelect());
      expect(await screen.findByText("Themes folder")).toBeInTheDocument();
      await user.keyboard("{Escape}");

      await user.click(screen.getByRole("button", { name: "More theme actions" }));
      expect(screen.queryByRole("menuitem", { name: "Remove Paper" })).toBeNull();
      await user.click(await screen.findByRole("menuitem", { name: "Open themes folder" }));
      expect(desktop.openThemeFolder).toHaveBeenCalledWith();
    });
  });

  it("lists what is wrong with a theme it cannot install", async () => {
    const user = userEvent.setup();
    render(<AppearanceTab />, { wrapper: Wrapper });

    await user.upload(folderInput(), themeFolder(PAPER, "body { background: url(https://evil.test/x); }"));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Couldn't install this theme")).toBeInTheDocument();
    expect(within(dialog).getByText(/theme\.css line 1: url\("https:\/\/evil.test\/x"\)/)).toBeInTheDocument();
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
  });

  it("renders every option type a theme declares, grouped under its headings", async () => {
    const user = userEvent.setup();
    useInstalledThemesStore.setState({
      themes: [
        {
          ...PAPER,
          css: PAPER_CSS,
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
        },
      ],
    });
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

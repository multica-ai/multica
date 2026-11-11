import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import {
  DEFAULT_APPEARANCE,
  parseTheme,
  useAppearanceStore,
  useInstalledThemesStore,
  writeZip,
} from "@multica/core/appearance";
import type { StoreTheme } from "@multica/core/api/schemas";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const api = vi.hoisted(() => ({
  listStoreThemes: vi.fn(),
  getStoreThemePackage: vi.fn(),
  getStoreThemeScreenshot: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api }));
vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: "light", resolvedTheme: "light", setTheme: vi.fn() }),
}));

import { ThemesButton } from "./themes-dialog";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function renderButton() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <I18nProvider locale="en" resources={TEST_RESOURCES}>
          {children}
        </I18nProvider>
      </QueryClientProvider>
    );
  }
  return render(<ThemesButton />, { wrapper: Wrapper });
}

const encoder = new TextEncoder();
const manifest = (id: string, name: string, version: string) => ({ apiVersion: 1, id, name, author: "Someone", version });
const css = ".theme-light { --page-canvas: #f6efdf; }";
const release = (id: string, name: string, version: string) =>
  writeZip([
    { path: "manifest.json", data: encoder.encode(JSON.stringify(manifest(id, name, version))) },
    { path: "theme.css", data: encoder.encode(css) },
  ]);
const listed = (id: string, name: string, version = "1.0.0", downloads = 0): StoreTheme => ({
  id,
  name,
  author: "Someone",
  repo: `someone/multica-theme-${id}`,
  description: `${name} theme`,
  modes: ["light", "dark"],
  version,
  size: 100,
  screenshots: ["light", "dark"],
  downloads,
});

describe("ThemesButton", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
    useInstalledThemesStore.setState({ themes: [], folders: {}, fromStore: {} });
    configStore.getState().setThemeStoreEnabled(true);
    api.listStoreThemes.mockResolvedValue([listed("ink", "Ink"), listed("sand", "Sand")]);
    api.getStoreThemeScreenshot.mockResolvedValue(new Blob(["png"], { type: "image/png" }));
    api.getStoreThemePackage.mockImplementation(async (id: string) =>
      release(id, id === "ink" ? "Ink" : "Sand", "1.0.0"),
    );
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("lists only the installed themes and makes no requests when the server has no store", async () => {
    configStore.getState().setThemeStoreEnabled(false);
    const user = userEvent.setup();
    const { rerender } = renderButton();
    expect(screen.queryByRole("button", { name: "Manage" })).toBeNull();

    const parsed = parseTheme({ manifest: manifest("paper", "Paper", "1.0.0"), css });
    if (!parsed.ok) throw new Error("fixture");
    useInstalledThemesStore.getState().install(parsed.theme);
    rerender(<ThemesButton />);
    await user.click(screen.getByRole("button", { name: "Manage" }));
    expect(screen.getByText("The themes you installed.")).toBeTruthy();
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual(["Paper"]);
    expect(api.listStoreThemes).not.toHaveBeenCalled();
  });

  it("lists and searches the store, installs a theme and removes it", async () => {
    const user = userEvent.setup();
    renderButton();
    await user.click(screen.getByRole("button", { name: "Manage" }));
    expect(await screen.findByRole("heading", { name: "Ink" })).toBeTruthy();
    expect(screen.getByText("Beta")).toBeTruthy();
    expect(screen.getByText("Showing 2 themes")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Default" })).toBeNull();

    await user.type(screen.getByRole("textbox", { name: "Search themes" }), "san");
    expect(screen.queryByRole("heading", { name: "Ink" })).toBeNull();
    expect(screen.getByRole("heading", { name: "Sand" })).toBeTruthy();
    await user.clear(screen.getByRole("textbox", { name: "Search themes" }));

    const [installInk] = screen.getAllByRole("button", { name: "Install" });
    await user.click(installInk!);
    await waitFor(() => expect(useInstalledThemesStore.getState().fromStore).toEqual({ ink: "1.0.0" }));
    expect(useAppearanceStore.getState().theme).toBe("ink");
    expect(await screen.findByRole("button", { name: "In use" })).toBeTruthy();
    // The card stays where it was while the window is open.
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual(["Ink", "Sand"]);
    await user.click(screen.getByRole("switch", { name: "Show installed only" }));
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual(["Ink"]);

    await user.click(screen.getByRole("button", { name: "Remove Ink" }));
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
    expect(useAppearanceStore.getState().theme).toBe("default");
  });

  it("shows how often each store theme was downloaded, most downloaded first, and recounts after an install", async () => {
    api.listStoreThemes.mockResolvedValue([listed("ink", "Ink", "1.0.0", 1), listed("sand", "Sand", "1.0.0", 1250)]);
    const user = userEvent.setup();
    renderButton();
    await user.click(screen.getByRole("button", { name: "Manage" }));
    expect(await screen.findByText("by Someone · 1.3K downloads")).toBeTruthy();
    expect(screen.getByText("by Someone · 1 download")).toBeTruthy();
    expect(screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent)).toEqual(["Sand", "Ink"]);

    api.listStoreThemes.mockResolvedValue([listed("ink", "Ink", "1.0.0", 2), listed("sand", "Sand", "1.0.0", 1250)]);
    const [installSand] = screen.getAllByRole("button", { name: "Install" });
    await user.click(installSand!);
    expect(await screen.findByText("by Someone · 2 downloads")).toBeTruthy();
    expect(api.listStoreThemes).toHaveBeenCalledTimes(2);
  });

  it("offers the listed release as an update", async () => {
    const parsed = parseTheme({ manifest: manifest("ink", "Ink", "0.9.0"), css });
    if (!parsed.ok) throw new Error("fixture");
    useInstalledThemesStore.getState().installFromStore(parsed.theme, "0.9.0");
    const user = userEvent.setup();
    renderButton();
    await user.click(screen.getByRole("button", { name: "Manage" }));
    await user.click(await screen.findByRole("button", { name: "Update to 1.0.0" }));
    await waitFor(() => expect(useInstalledThemesStore.getState().fromStore).toEqual({ ink: "1.0.0" }));
    expect(useInstalledThemesStore.getState().themes[0]?.version).toBe("1.0.0");
  });

  it("installs nothing when the release fails the checks", async () => {
    api.getStoreThemePackage.mockResolvedValue(
      writeZip([
        { path: "manifest.json", data: encoder.encode(JSON.stringify(manifest("ink", "Ink", "1.0.0"))) },
        { path: "theme.css", data: encoder.encode("body { background: url(https://evil.test/x.png); }") },
      ]),
    );
    const user = userEvent.setup();
    renderButton();
    await user.click(screen.getByRole("button", { name: "Manage" }));
    await screen.findByRole("heading", { name: "Ink" });
    await user.click(screen.getAllByRole("button", { name: "Install" })[0]!);
    await waitFor(() => expect(api.getStoreThemePackage).toHaveBeenCalled());
    await waitFor(() => expect(screen.getAllByRole("button", { name: "Install" })).toHaveLength(2));
    expect(useInstalledThemesStore.getState().themes).toEqual([]);
  });
});

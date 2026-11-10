// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { StoreTheme } from "../api/schemas";
import { INSTALLED_THEMES_STORAGE_KEY, useInstalledThemesStore } from "./installed-themes";
import { writeZip } from "./theme-archive";
import { parseTheme } from "./theme-package";
import { readStoreTheme, storeThemeState } from "./theme-store";

vi.mock("../api", () => ({ api: { getStoreThemePackage: vi.fn() } }));

const encoder = new TextEncoder();
const manifest = (id: string, version: string) => ({ apiVersion: 1, id, name: "Ink", author: "Someone", version });
const zip = (id: string, version: string, css = ".theme-light { --page-canvas: #f6efdf; }") =>
  writeZip([
    { path: `${id}/manifest.json`, data: encoder.encode(JSON.stringify(manifest(id, version))) },
    { path: `${id}/theme.css`, data: encoder.encode(css) },
  ]);
const entry = (overrides: Partial<StoreTheme> = {}): StoreTheme => ({
  id: "ink",
  name: "Ink",
  author: "Someone",
  repo: "someone/multica-theme-ink",
  modes: ["light", "dark"],
  version: "1.0.0",
  size: 100,
  screenshots: ["light", "dark"],
  ...overrides,
});
const theme = (id: string, version = "1.0.0") => {
  const parsed = parseTheme({ manifest: manifest(id, version), css: "" });
  if (!parsed.ok) throw new Error("fixture");
  return parsed.theme;
};

describe("readStoreTheme", () => {
  it("reads the release like an imported theme", async () => {
    vi.mocked(api.getStoreThemePackage).mockResolvedValue(zip("ink", "1.0.0"));
    const result = await readStoreTheme(entry());
    expect(result.ok && result.theme.id).toBe("ink");
  });

  it("runs the same CSS checks as an import", async () => {
    vi.mocked(api.getStoreThemePackage).mockResolvedValue(
      zip("ink", "1.0.0", "body { background: url(https://evil.test/x.png); }"),
    );
    const result = await readStoreTheme(entry());
    expect(result.ok).toBe(false);
  });

  it("refuses a package that is another theme or another release", async () => {
    vi.mocked(api.getStoreThemePackage).mockResolvedValue(zip("other", "1.0.0"));
    expect((await readStoreTheme(entry())).ok).toBe(false);
    vi.mocked(api.getStoreThemePackage).mockResolvedValue(zip("ink", "0.9.0"));
    expect((await readStoreTheme(entry())).ok).toBe(false);
  });
});

describe("store installs", () => {
  beforeEach(() => {
    localStorage.clear();
    useInstalledThemesStore.setState({ themes: [], folders: {}, fromStore: {} });
  });
  const state = () => {
    const { themes, fromStore } = useInstalledThemesStore.getState();
    return storeThemeState(entry(), { themes, fromStore });
  };

  it("offers an update when the store lists another release", () => {
    expect(state()).toBe("available");
    useInstalledThemesStore.getState().installFromStore(theme("ink", "0.9.0"), "0.9.0");
    expect(state()).toBe("update");
    useInstalledThemesStore.getState().installFromStore(theme("ink"), "1.0.0");
    expect(state()).toBe("installed");
    useInstalledThemesStore.getState().uninstall("ink");
    expect(state()).toBe("available");
    expect(useInstalledThemesStore.getState().fromStore).toEqual({});
  });

  it("does not take over a built-in, imported or themes-folder theme with the same id", () => {
    expect(storeThemeState(entry({ id: "default" }), { themes: [], fromStore: {} })).toBe("taken");
    useInstalledThemesStore.getState().install(theme("ink"));
    expect(state()).toBe("taken");
    useInstalledThemesStore.getState().installFromStore(theme("ink"), "1.0.0");
    useInstalledThemesStore.getState().syncFolder([{ folder: "ink", result: { ok: true, theme: theme("ink", "2.0.0") } }]);
    expect(state()).toBe("taken");
    expect(useInstalledThemesStore.getState().fromStore).toEqual({});
  });

  it("forgets the store release when the theme is imported over it", () => {
    useInstalledThemesStore.getState().installFromStore(theme("ink"), "1.0.0");
    useInstalledThemesStore.getState().install(theme("ink", "1.1.0"));
    expect(useInstalledThemesStore.getState().fromStore).toEqual({});
  });

  it("keeps the store release across reloads, for installed themes only", async () => {
    const parsed = parseTheme({ manifest: manifest("ink", "1.0.0"), css: ".theme-light { --page-canvas: #f6efdf; }" });
    if (!parsed.ok) throw new Error("fixture");
    const stored = parsed.theme;
    localStorage.setItem(
      INSTALLED_THEMES_STORAGE_KEY,
      JSON.stringify({ state: { themes: [stored], folders: {}, fromStore: { ink: "1.0.0", gone: "1.0.0", bad: 3 } }, version: 0 }),
    );
    await useInstalledThemesStore.persist.rehydrate();
    expect(useInstalledThemesStore.getState().fromStore).toEqual({ ink: "1.0.0" });
  });
});

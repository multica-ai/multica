// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  APPEARANCE_STORAGE_KEY,
  BUILT_IN_THEMES,
  DEFAULT_APPEARANCE,
  activeTheme,
  appearanceRootVariables,
  appearanceStylesheet,
  appearanceThemeAttributes,
  appearanceThemeCss,
  findBuiltInTheme,
  isDefaultAppearance,
  isSafeFontValue,
  isSafeThemeValue,
  localizedText,
  sanitizeAppearance,
  sanitizeThemeCss,
  THEME_VARIABLE_NAMES,
  themeModeVariables,
  themeTokensCss,
  useAppearanceStore,
  useInstalledThemesStore,
  type AppearancePreferences,
  type Theme,
} from ".";

const pop = findBuiltInTheme("pop")!;
const defaults = findBuiltInTheme("default")!;

describe("appearance store", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState(DEFAULT_APPEARANCE);
  });

  it("saves the preferences on this device", () => {
    useAppearanceStore.getState().set({ theme: "github", fontSize: 16, contentSize: 17 });
    const saved = JSON.parse(localStorage.getItem(APPEARANCE_STORAGE_KEY) ?? "{}");
    expect(saved.state).toMatchObject({ theme: "github", fontSize: 16, contentSize: 17 });
  });

  it("keeps option values per theme", () => {
    const { setThemeOption } = useAppearanceStore.getState();
    setThemeOption("pop", "signal", "mint");
    setThemeOption("pop", "status-blocks", false);
    expect(useAppearanceStore.getState().themeOptions).toEqual({
      pop: { signal: "mint", "status-blocks": false },
    });
  });

  it("resets everything to the defaults", () => {
    const { set, setThemeOption, reset } = useAppearanceStore.getState();
    set({ theme: "pop", contentFont: "serif" });
    setThemeOption("pop", "signal", "sky");
    reset();
    expect(isDefaultAppearance(sanitizeAppearance(useAppearanceStore.getState()))).toBe(true);
  });

  it("reads older saved shapes", async () => {
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "pop", cjkFont: "kai", accent: "violet" }, version: 0 }),
    );
    await useAppearanceStore.persist.rehydrate();
    expect(useAppearanceStore.getState()).toMatchObject({ theme: "pop", uiCjkFont: "kai" });
    expect(useAppearanceStore.getState()).not.toHaveProperty("accent");
  });
});

describe("sanitizeAppearance", () => {
  it("falls back per field for unknown or missing values", () => {
    expect(
      sanitizeAppearance({
        theme: "Not An Id",
        fontSize: 40,
        density: "comfortable",
        reduceMotion: "yes",
        contentSize: 99,
        uiCjkFont: 'local:Evil"; }',
      }),
    ).toEqual({ ...DEFAULT_APPEARANCE, density: "comfortable" });
    expect(sanitizeAppearance(null)).toEqual(DEFAULT_APPEARANCE);
  });

  it("keeps the id of a theme that may be installed later", () => {
    expect(sanitizeAppearance({ theme: "solarized" }).theme).toBe("solarized");
  });

  it("accepts installed CJK families and follow-the-interface values", () => {
    expect(
      sanitizeAppearance({ uiCjkFont: "local:Hiragino Sans GB", contentCjkFont: "ui", contentFont: "serif" }),
    ).toMatchObject({ uiCjkFont: "local:Hiragino Sans GB", contentCjkFont: "ui", contentFont: "serif" });
  });

  it("drops malformed theme options", () => {
    expect(
      sanitizeAppearance({ themeOptions: { pop: { signal: "mint", "Bad Key": true, big: "x".repeat(301) }, "!": {} } })
        .themeOptions,
    ).toEqual({ pop: { signal: "mint" } });
  });
});

describe("appearanceRootVariables", () => {
  it("leaves every variable unset for the defaults", () => {
    expect(Object.values(appearanceRootVariables(DEFAULT_APPEARANCE)).every((v) => v === null)).toBe(true);
  });

  it("maps interface and content type onto separate variables", () => {
    const vars = appearanceRootVariables({
      ...DEFAULT_APPEARANCE,
      uiFont: "serif",
      uiCjkFont: "kai",
      contentFont: "inter",
      contentCjkFont: "local:Songti SC",
      contentSize: 16,
      contentLeading: "relaxed",
      codeFont: "jetbrains",
      fontSize: 15,
      density: "compact",
    });
    expect(vars["--font-ui"]).toBe("var(--font-ui-serif)");
    expect(vars["--font-cjk"]).toBe("var(--font-cjk-kai)");
    expect(vars["--font-content-latin"]).toBe("var(--font-ui-inter)");
    expect(vars["--font-content-cjk"]).toBe('"Songti SC", var(--font-cjk-sans)');
    expect(vars["--content-scale"]).toBe(String(16 / 14));
    expect(vars["--content-leading"]).toBe("1.8");
    expect(vars["--font-mono"]).toMatch(/^var\(--font-code-jetbrains\)/);
    expect(vars["--font-scale"]).toBe(String(15 / 14));
    expect(vars["--issue-row-height"]).toBe("32px");
  });
});

describe("appearanceStylesheet", () => {
  it("is empty for the defaults", () => {
    expect(appearanceStylesheet(defaults, DEFAULT_APPEARANCE)).toBe("");
    expect(appearanceThemeAttributes(defaults, DEFAULT_APPEARANCE)).toEqual({ "data-theme": null });
  });

  it("writes contrast with selectors that outrank the theme's", () => {
    const prefs = { ...DEFAULT_APPEARANCE, theme: "github", highContrast: true } as const;
    const css = appearanceStylesheet(activeTheme(prefs), prefs);
    expect(css).not.toContain("--brand");
    expect(css).toMatch(/:root:not\(\.dark\), :root\.dark \{ --muted-foreground: color-mix/);
    expect(activeTheme(prefs).css).toMatch(/^\.theme-light \{/);
  });

  it("names the theme and its options, defaults included", () => {
    const prefs: AppearancePreferences = {
      ...DEFAULT_APPEARANCE,
      theme: "pop",
      themeOptions: { pop: { signal: "sky", "status-blocks": false, unknown: true } },
    };
    expect(appearanceThemeAttributes(pop, prefs)).toEqual({
      "data-theme": "pop",
      "data-theme-option-signal": "sky",
      "data-theme-option-offset-shadow": "on",
      "data-theme-option-status-blocks": null,
      "data-theme-option-outlined-chips": "on",
    });
  });

  it("writes option variables per mode, from valid choices or the defaults", () => {
    const theme: Theme = {
      apiVersion: 1,
      id: "paper",
      name: "Paper",
      css: "",
      options: [
        { id: "paper-color", type: "color", label: "Paper", default: { light: "#f6efdf", dark: "#1d1b16" } },
        { id: "page-width", type: "slider", label: "Width", default: 46, min: 30, max: 60, unit: "rem" },
        { id: "h1-style", type: "text", label: "H1", default: "normal" },
        { id: "hyphens", type: "toggle", label: "Hyphens", default: true },
      ],
    };
    const prefs: AppearancePreferences = {
      ...DEFAULT_APPEARANCE,
      themeOptions: { paper: { "paper-color": { dark: "#000000" }, "page-width": 99, "h1-style": "x; }" } },
    };
    expect(appearanceStylesheet(theme, prefs)).toBe(
      [
        ":root.theme-light { --paper-color: #f6efdf; --page-width: 60rem; --h1-style: normal; }",
        ":root.theme-dark { --paper-color: #000000; --page-width: 60rem; --h1-style: normal; }",
      ].join("\n"),
    );
    expect(appearanceThemeAttributes(theme, prefs)).toEqual({
      "data-theme": "paper",
      "data-theme-option-hyphens": "on",
    });
  });

  it("keeps stored option values of every type", () => {
    const stored = sanitizeAppearance({
      themeOptions: { paper: { a: true, b: "sky", c: 1.5, d: { light: "#fff", dark: 3 }, e: Number.NaN, f: [] } },
    });
    expect(stored.themeOptions).toEqual({ paper: { a: true, b: "sky", c: 1.5, d: { light: "#fff" } } });
  });

  it("falls back to the default theme in safe mode or when the theme is missing", () => {
    expect(activeTheme({ ...DEFAULT_APPEARANCE, theme: "pop" }, true).id).toBe("default");
    expect(activeTheme({ ...DEFAULT_APPEARANCE, theme: "not-installed" }).id).toBe("default");
  });

  it("applies an installed theme and its CSS", () => {
    const theme: Theme = {
      apiVersion: 1,
      id: "paper",
      name: "Paper",
      css: '.theme-light { --page-canvas: oklch(0.97 0.02 85); }\n[data-mc="content"] p { hyphens: auto; }',
    };
    useInstalledThemesStore.setState({ themes: [theme] });
    const active = activeTheme({ ...DEFAULT_APPEARANCE, theme: "paper" });
    expect(active).toBe(theme);
    expect(appearanceThemeCss(active)).toContain("hyphens: auto");
    useInstalledThemesStore.setState({ themes: [] });
  });
});

describe("built-in themes", () => {
  it("set only public variables, and their CSS passes the same check as installed themes", () => {
    expect(new Set(BUILT_IN_THEMES.map((t) => t.id)).size).toBe(BUILT_IN_THEMES.length);
    for (const theme of BUILT_IN_THEMES) {
      for (const tokens of [theme.tokens.light, theme.tokens.dark, theme.frame?.light, theme.frame?.dark]) {
        for (const [name, value] of Object.entries(tokens ?? {})) {
          expect(THEME_VARIABLE_NAMES.has(name), name).toBe(true);
          expect(isSafeFontValue(value) || isSafeThemeValue(value), `${name}: ${value}`).toBe(true);
        }
      }
      expect(sanitizeThemeCss(theme.css, theme.assets)).toEqual({ ok: true, css: theme.css });
    }
  });

  it("leave Han to the CJK choice: Latin font stacks name no generic family", () => {
    const generic = /^(?:serif|sans-serif|monospace|cursive|fantasy|system-ui|math|emoji|fangsong|ui-[a-z-]+)$/;
    const latin = ["font-ui", "font-content-latin"];
    for (const theme of BUILT_IN_THEMES) {
      const stacks = [theme.tokens.light, theme.tokens.dark].flatMap((tokens) =>
        latin.flatMap((name) => (tokens[name] ? [tokens[name]] : [])),
      );
      for (const option of theme.options ?? []) {
        if (option.type === "variable-select" && latin.includes(option.id)) {
          stacks.push(...option.choices.map((choice) => choice.value));
        }
      }
      for (const stack of stacks) {
        const families = stack.split(",").map((family) => family.trim());
        expect(families.filter((family) => generic.test(family)), `${theme.id}: ${stack}`).toEqual([]);
      }
    }
  });

  it("compile their token tables to mode rules a preview can read back", () => {
    const github = findBuiltInTheme("github")!;
    expect(themeModeVariables(github.css).light).toEqual(github.tokens.light);
    expect(themeModeVariables(github.css).dark).toEqual(github.tokens.dark);
    expect(themeTokensCss({ tokens: { light: { surface: "oklch(1 0 0)" }, dark: {} } })).toBe(
      ".theme-light { --surface: oklch(1 0 0); }",
    );
  });

  it("give Pop's options a label in every locale the app ships", () => {
    for (const option of pop.options ?? []) {
      for (const locale of ["en", "zh-Hans", "zh-Hant", "ja", "ko", "es", "fr"]) {
        expect(localizedText(option.label, locale)).not.toBe("");
      }
    }
    expect(localizedText(pop.options![0]!.label, "zh-Hant")).toBe("信號色");
    expect(localizedText(pop.options![0]!.label, "de")).toBe("Signal color");
  });
});

describe("isSafeFontValue", () => {
  it("accepts font stacks and rejects anything that can leave the declaration", () => {
    expect(isSafeFontValue('"Source Serif 4", Georgia, var(--font-ui-serif)')).toBe(true);
    expect(isSafeFontValue('"霞鹜文楷", serif')).toBe(true);
    expect(isSafeFontValue('"Open')).toBe(false);
    expect(isSafeFontValue("Inter; color: red")).toBe(false);
    expect(isSafeFontValue("url(x)")).toBe(false);
    expect(isSafeFontValue("Inter}")).toBe(false);
  });
});

/**
 * Themes are data, not stylesheets. A theme overrides design tokens from
 * `packages/ui/styles/tokens.css` per mode, may repaint the frame around the
 * page (rail, desktop toolbar and tab bar), and may opt into a fixed set of
 * component treatments that `packages/ui/styles/appearance.css` implements.
 *
 * Keeping the format to tokens and named traits is what lets a theme come
 * from outside the codebase later: a manifest can only set allowlisted token
 * names to sanitised values, so installing one cannot inject arbitrary CSS.
 */

/** Tokens a theme may set. Anything else in a manifest is ignored. */
export const THEME_TOKENS = [
  "app-shell",
  "page-canvas",
  "surface",
  "surface-foreground",
  "surface-raised",
  "surface-hover",
  "surface-selected",
  "surface-selected-foreground",
  "surface-border",
  "surface-shadow",
  "floating-shadow",
  "menu-shadow",
  "background",
  "foreground",
  "card",
  "card-foreground",
  "popover",
  "popover-foreground",
  "primary",
  "primary-foreground",
  "secondary",
  "secondary-foreground",
  "muted",
  "muted-foreground",
  "faint-foreground",
  "accent",
  "accent-foreground",
  "border",
  "input",
  "ring",
  "radius",
  "sidebar",
  "sidebar-foreground",
  "sidebar-accent",
  "sidebar-accent-foreground",
  "sidebar-border",
  "brand",
  "brand-foreground",
  "button-primary",
  "button-primary-foreground",
  "button-primary-border",
] as const;
export type ThemeToken = (typeof THEME_TOKENS)[number];
export type ThemeTokens = Partial<Record<ThemeToken, string>>;

/**
 * Component treatments a theme can switch on. Each one is a block in
 * appearance.css keyed on `data-theme-traits~="<trait>"`.
 * - `offset-shadow`: primary buttons lift onto a hard offset shadow on hover.
 * - `status-blocks`: issue group headers become solid blocks per status.
 * - `outlined-chips`: label chips get an ink outline.
 */
export const THEME_TRAITS = ["offset-shadow", "status-blocks", "outlined-chips"] as const;
export type ThemeTrait = (typeof THEME_TRAITS)[number];

export interface ThemeManifest {
  id: string;
  name: string;
  /** Who made it; built-in themes name the product they follow. */
  author?: string;
  tokens: { light: ThemeTokens; dark: ThemeTokens };
  /** Tokens for what sits on the frame: the rail, desktop toolbar and tab bar. */
  frame?: { light?: ThemeTokens; dark?: ThemeTokens };
  traits?: readonly ThemeTrait[];
}

export const DEFAULT_THEME_ID = "default";

const INK = "oklch(0.145 0 0)";
const POP_YELLOW = "oklch(0.9 0.17 98)";

/** Pop's frame is black in both modes, so what sits on it reads light tokens. */
const POP_FRAME: ThemeTokens = {
  foreground: "oklch(0.97 0 0)",
  "muted-foreground": "oklch(0.8 0 0)",
  "faint-foreground": "oklch(0.64 0 0)",
  background: INK,
  "page-canvas": "oklch(0.25 0 0)",
  surface: "oklch(0.2 0 0)",
  "surface-raised": "oklch(0.25 0 0)",
  "surface-hover": "oklch(0.3 0 0)",
  "surface-selected": "oklch(0.32 0.05 98)",
  muted: "oklch(0.27 0 0)",
  "sidebar-accent": "oklch(0.27 0 0)",
  "sidebar-accent-foreground": POP_YELLOW,
  "surface-border": "oklch(0.4 0 0)",
  "surface-shadow": "none",
  ring: POP_YELLOW,
};

/**
 * Pop: black frame, warm paper canvas, ink outlines, small radius, hard
 * offset shadows and a signal-yellow primary. --primary stays ink because it
 * also paints text (`text-primary`); the yellow reaches buttons through
 * --button-primary.
 */
const POP: ThemeManifest = {
  id: "pop",
  name: "Pop",
  tokens: {
    light: {
      radius: "0.25rem",
      "app-shell": INK,
      "page-canvas": "oklch(0.974 0.008 95)",
      surface: "oklch(1 0 0)",
      "surface-raised": "oklch(1 0 0)",
      "surface-hover": "oklch(0.945 0.014 95)",
      "surface-selected": "oklch(0.93 0.08 98)",
      "surface-border": INK,
      "surface-shadow": `2px 2px 0 ${INK}`,
      "floating-shadow": `6px 6px 0 ${INK}`,
      "menu-shadow": `4px 4px 0 ${INK}`,
      secondary: "oklch(0.945 0.014 95)",
      muted: "oklch(0.945 0.014 95)",
      accent: "oklch(0.945 0.014 95)",
      "muted-foreground": "oklch(0.44 0.012 95)",
      "faint-foreground": "oklch(0.56 0.012 95)",
      border: INK,
      input: INK,
      ring: INK,
      sidebar: "oklch(0.945 0.014 95)",
      "sidebar-accent": "oklch(0.91 0.02 95)",
      "sidebar-border": INK,
      brand: "oklch(0.5 0.2 262)",
      "button-primary": POP_YELLOW,
      "button-primary-foreground": INK,
      "button-primary-border": INK,
    },
    dark: {
      radius: "0.25rem",
      "app-shell": "oklch(0 0 0)",
      "page-canvas": "oklch(0.17 0 0)",
      surface: "oklch(0.2 0 0)",
      "surface-raised": "oklch(0.22 0 0)",
      "surface-hover": "oklch(0.26 0 0)",
      "surface-selected": "oklch(0.32 0.06 98)",
      "surface-border": "oklch(0.92 0 0)",
      "surface-shadow": `2px 2px 0 oklch(0.92 0 0)`,
      "floating-shadow": `6px 6px 0 ${POP_YELLOW}`,
      "menu-shadow": `4px 4px 0 ${POP_YELLOW}`,
      secondary: "oklch(0.26 0 0)",
      muted: "oklch(0.26 0 0)",
      accent: "oklch(0.26 0 0)",
      "muted-foreground": "oklch(0.8 0 0)",
      "faint-foreground": "oklch(0.62 0 0)",
      border: "oklch(0.92 0 0)",
      input: "oklch(0.92 0 0)",
      ring: POP_YELLOW,
      sidebar: "oklch(0.2 0 0)",
      "sidebar-accent": "oklch(0.26 0 0)",
      "sidebar-border": "oklch(0.92 0 0)",
      brand: "oklch(0.72 0.15 262)",
      "button-primary": POP_YELLOW,
      "button-primary-foreground": INK,
      "button-primary-border": "oklch(0.92 0 0)",
    },
  },
  frame: { light: POP_FRAME, dark: POP_FRAME },
  traits: ["offset-shadow", "status-blocks", "outlined-chips"],
};

/** GitHub (Primer): cool greys, 6px radius, green primary, blue links. */
const GITHUB: ThemeManifest = {
  id: "github",
  name: "GitHub",
  author: "Primer",
  tokens: {
    light: {
      radius: "0.375rem",
      "app-shell": "oklch(0.978 0.003 247.9)",
      "page-canvas": "oklch(1 0 0)",
      surface: "oklch(1 0 0)",
      "surface-raised": "oklch(1 0 0)",
      "surface-hover": "oklch(0.96 0.005 247.9)",
      "surface-selected": "oklch(0.948 0.005 247.9)",
      "surface-border": "oklch(0.881 0.013 244.3)",
      "surface-shadow": "0 1px 0 rgb(31 35 40 / 0.04)",
      foreground: "oklch(0.254 0.011 254)",
      "surface-foreground": "oklch(0.254 0.011 254)",
      "surface-selected-foreground": "oklch(0.254 0.011 254)",
      "card-foreground": "oklch(0.254 0.011 254)",
      "popover-foreground": "oklch(0.254 0.011 254)",
      primary: "oklch(0.254 0.011 254)",
      secondary: "oklch(0.978 0.003 247.9)",
      muted: "oklch(0.978 0.003 247.9)",
      accent: "oklch(0.96 0.005 247.9)",
      "muted-foreground": "oklch(0.495 0.022 250.8)",
      "faint-foreground": "oklch(0.596 0.022 253)",
      border: "oklch(0.916 0.009 247.9)",
      input: "oklch(0.881 0.013 244.3)",
      ring: "oklch(0.54 0.191 257.5)",
      sidebar: "oklch(0.978 0.003 247.9)",
      "sidebar-accent": "oklch(0.935 0.008 253.9)",
      "sidebar-border": "oklch(0.881 0.013 244.3)",
      brand: "oklch(0.54 0.191 257.5)",
      "button-primary": "oklch(0.552 0.145 148.2)",
      "button-primary-foreground": "oklch(1 0 0)",
      "button-primary-border": "oklch(0.5 0.13 148)",
    },
    dark: {
      radius: "0.375rem",
      "app-shell": "oklch(0.104 0.019 248.3)",
      "page-canvas": "oklch(0.176 0.014 258.4)",
      surface: "oklch(0.22 0.018 255.7)",
      "surface-raised": "oklch(0.22 0.018 255.7)",
      "surface-hover": "oklch(0.274 0.018 251.9)",
      "surface-selected": "oklch(0.292 0.02 260.6)",
      "surface-border": "oklch(0.384 0.018 254.7)",
      foreground: "oklch(0.97 0.01 247.9)",
      "surface-foreground": "oklch(0.97 0.01 247.9)",
      "surface-selected-foreground": "oklch(0.97 0.01 247.9)",
      "card-foreground": "oklch(0.97 0.01 247.9)",
      "popover-foreground": "oklch(0.97 0.01 247.9)",
      secondary: "oklch(0.274 0.018 251.9)",
      muted: "oklch(0.22 0.018 255.7)",
      accent: "oklch(0.274 0.018 251.9)",
      "muted-foreground": "oklch(0.706 0.015 254.6)",
      "faint-foreground": "oklch(0.604 0.021 258.4)",
      border: "oklch(0.326 0.016 255.6)",
      input: "oklch(0.384 0.018 254.7)",
      ring: "oklch(0.663 0.169 255.9)",
      sidebar: "oklch(0.176 0.014 258.4)",
      "sidebar-accent": "oklch(0.274 0.018 251.9)",
      "sidebar-border": "oklch(0.326 0.016 255.6)",
      brand: "oklch(0.663 0.169 255.9)",
      "button-primary": "oklch(0.546 0.147 146.3)",
      "button-primary-foreground": "oklch(1 0 0)",
      "button-primary-border": "oklch(0.6 0.15 146)",
    },
  },
};

/** Blueprint: crisp blue-greys, 4px radius, blue intent for primary and selection. */
const BLUEPRINT: ThemeManifest = {
  id: "blueprint",
  name: "Blueprint",
  author: "Palantir",
  tokens: {
    light: {
      radius: "0.25rem",
      "app-shell": "oklch(0.951 0.005 258.3)",
      "page-canvas": "oklch(0.976 0.003 264.5)",
      surface: "oklch(1 0 0)",
      "surface-raised": "oklch(1 0 0)",
      "surface-hover": "oklch(0.93 0.005 247.9)",
      "surface-selected": "oklch(0.94 0.02 257.5)",
      "surface-border": "oklch(0.88 0.01 252.8)",
      "surface-shadow": "0 0 0 1px rgb(17 20 24 / 0.04), 0 1px 1px rgb(17 20 24 / 0.12)",
      "menu-shadow": "0 0 0 1px rgb(17 20 24 / 0.1), 0 2px 4px rgb(17 20 24 / 0.2), 0 8px 24px rgb(17 20 24 / 0.2)",
      foreground: "oklch(0.245 0.014 253.1)",
      "surface-foreground": "oklch(0.245 0.014 253.1)",
      "surface-selected-foreground": "oklch(0.245 0.014 253.1)",
      "card-foreground": "oklch(0.245 0.014 253.1)",
      "popover-foreground": "oklch(0.245 0.014 253.1)",
      primary: "oklch(0.245 0.014 253.1)",
      secondary: "oklch(0.951 0.005 258.3)",
      muted: "oklch(0.951 0.005 258.3)",
      accent: "oklch(0.93 0.005 247.9)",
      "muted-foreground": "oklch(0.5 0.031 257.1)",
      "faint-foreground": "oklch(0.595 0.03 255.2)",
      border: "oklch(0.905 0.008 253.9)",
      input: "oklch(0.84 0.013 255.5)",
      ring: "oklch(0.56 0.163 257.6)",
      sidebar: "oklch(0.951 0.005 258.3)",
      "sidebar-accent": "oklch(0.905 0.008 253.9)",
      "sidebar-border": "oklch(0.88 0.01 252.8)",
      brand: "oklch(0.487 0.146 257.7)",
      "button-primary": "oklch(0.56 0.163 257.6)",
      "button-primary-foreground": "oklch(1 0 0)",
      "button-primary-border": "oklch(0.487 0.146 257.7)",
    },
    dark: {
      radius: "0.25rem",
      "app-shell": "oklch(0.245 0.014 253.1)",
      "page-canvas": "oklch(0.283 0.015 256.8)",
      surface: "oklch(0.323 0.016 259.8)",
      "surface-raised": "oklch(0.362 0.018 258.4)",
      "surface-hover": "oklch(0.362 0.018 258.4)",
      "surface-selected": "oklch(0.377 0.057 259.3)",
      "surface-border": "oklch(0.399 0.023 258.4)",
      foreground: "oklch(0.976 0.003 264.5)",
      "surface-foreground": "oklch(0.976 0.003 264.5)",
      "surface-selected-foreground": "oklch(0.976 0.003 264.5)",
      "card-foreground": "oklch(0.976 0.003 264.5)",
      "popover-foreground": "oklch(0.976 0.003 264.5)",
      secondary: "oklch(0.362 0.018 258.4)",
      muted: "oklch(0.323 0.016 259.8)",
      accent: "oklch(0.362 0.018 258.4)",
      "muted-foreground": "oklch(0.8 0.019 258.4)",
      "faint-foreground": "oklch(0.7 0.025 258.4)",
      border: "oklch(0.369 0.017 258.4)",
      input: "oklch(0.45 0.025 258.4)",
      ring: "oklch(0.655 0.159 257.4)",
      sidebar: "oklch(0.245 0.014 253.1)",
      "sidebar-accent": "oklch(0.323 0.016 259.8)",
      "sidebar-border": "oklch(0.399 0.023 258.4)",
      brand: "oklch(0.76 0.12 257.4)",
      "button-primary": "oklch(0.56 0.163 257.6)",
      "button-primary-foreground": "oklch(1 0 0)",
      "button-primary-border": "oklch(0.655 0.159 257.4)",
    },
  },
};

/** Notion: warm greys, quiet hairlines, soft layered shadows, blue primary. */
const NOTION: ThemeManifest = {
  id: "notion",
  name: "Notion",
  tokens: {
    light: {
      radius: "0.375rem",
      "app-shell": "oklch(0.976 0.003 106.4)",
      "page-canvas": "oklch(1 0 0)",
      surface: "oklch(1 0 0)",
      "surface-raised": "oklch(1 0 0)",
      "surface-hover": "oklch(0.958 0.003 106.4)",
      "surface-selected": "oklch(0.943 0.003 106.5)",
      "surface-border": "oklch(0.933 0.003 106.5)",
      "surface-shadow": "0 1px 2px rgb(15 15 15 / 0.04)",
      "floating-shadow": "0 0 0 1px rgb(15 15 15 / 0.05), 0 3px 6px rgb(15 15 15 / 0.1), 0 9px 24px rgb(15 15 15 / 0.2)",
      "menu-shadow": "0 0 0 1px rgb(15 15 15 / 0.05), 0 3px 6px rgb(15 15 15 / 0.1), 0 9px 24px rgb(15 15 15 / 0.2)",
      foreground: "oklch(0.329 0.011 91.7)",
      "surface-foreground": "oklch(0.329 0.011 91.7)",
      "surface-selected-foreground": "oklch(0.329 0.011 91.7)",
      "card-foreground": "oklch(0.329 0.011 91.7)",
      "popover-foreground": "oklch(0.329 0.011 91.7)",
      primary: "oklch(0.329 0.011 91.7)",
      secondary: "oklch(0.958 0.003 106.4)",
      muted: "oklch(0.958 0.003 106.4)",
      accent: "oklch(0.958 0.003 106.4)",
      "muted-foreground": "oklch(0.51 0.006 95.2)",
      "faint-foreground": "oklch(0.617 0.005 91.5)",
      border: "oklch(0.946 0.001 106.4)",
      input: "oklch(0.894 0.003 106.5)",
      ring: "oklch(0.606 0.167 252.7)",
      sidebar: "oklch(0.976 0.003 106.4)",
      "sidebar-accent": "oklch(0.943 0.003 106.5)",
      "sidebar-border": "oklch(0.933 0.003 106.5)",
      brand: "oklch(0.541 0.153 254)",
      "button-primary": "oklch(0.541 0.153 254)",
      "button-primary-foreground": "oklch(1 0 0)",
    },
    dark: {
      radius: "0.375rem",
      "app-shell": "oklch(0.244 0 0)",
      "page-canvas": "oklch(0.213 0 0)",
      surface: "oklch(0.264 0 0)",
      "surface-raised": "oklch(0.264 0 0)",
      "surface-hover": "oklch(0.293 0 0)",
      "surface-selected": "oklch(0.305 0 0)",
      "surface-border": "oklch(0.337 0 0)",
      foreground: "oklch(0.87 0 0)",
      "surface-foreground": "oklch(0.87 0 0)",
      "surface-selected-foreground": "oklch(0.87 0 0)",
      "card-foreground": "oklch(0.87 0 0)",
      "popover-foreground": "oklch(0.87 0 0)",
      secondary: "oklch(0.293 0 0)",
      muted: "oklch(0.264 0 0)",
      accent: "oklch(0.293 0 0)",
      "muted-foreground": "oklch(0.72 0 0)",
      "faint-foreground": "oklch(0.6 0 0)",
      border: "oklch(0.285 0 0)",
      input: "oklch(0.364 0 0)",
      ring: "oklch(0.664 0.101 238.1)",
      sidebar: "oklch(0.244 0 0)",
      "sidebar-accent": "oklch(0.293 0 0)",
      "sidebar-border": "oklch(0.305 0 0)",
      brand: "oklch(0.7 0.1 238.1)",
      "button-primary": "oklch(0.541 0.153 254)",
      "button-primary-foreground": "oklch(1 0 0)",
    },
  },
};

const DEFAULT: ThemeManifest = {
  id: DEFAULT_THEME_ID,
  name: "Default",
  tokens: { light: {}, dark: {} },
};

export const BUILT_IN_THEMES: readonly ThemeManifest[] = [DEFAULT, POP, GITHUB, BLUEPRINT, NOTION];

export function findTheme(id: string): ThemeManifest | undefined {
  return BUILT_IN_THEMES.find((theme) => theme.id === id);
}

const SAFE_VALUE = /^[\w\s#.,%()/+-]+$/;
const UNSAFE_FUNCTION = /\b(url|image|image-set|cross-fade|element|expression|attr|env)\s*\(/i;

/** A token value a theme may set: colours, lengths, shadows and var() references only. */
export function isSafeThemeValue(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= 200 &&
    SAFE_VALUE.test(value) &&
    !UNSAFE_FUNCTION.test(value)
  );
}

function declarations(tokens: ThemeTokens | undefined): string {
  if (!tokens) return "";
  return THEME_TOKENS.filter((name) => isSafeThemeValue(tokens[name]))
    .map((name) => `--${name}: ${tokens[name]};`)
    .join(" ");
}

const FRAME_SLOTS =
  ':is([data-slot="app-rail"], [data-slot="window-toolbar"], [data-slot="main-top-bar"])';

/**
 * CSS for one theme. Light rules exclude `.dark` so both selectors are
 * equally specific and each mode reads only its own block; both outrank the
 * `:root` and `.dark` blocks of tokens.css.
 */
export function themeStylesheet(theme: ThemeManifest): string {
  const blocks: string[] = [];
  const add = (selector: string, body: string) => {
    if (body) blocks.push(`${selector} { ${body} }`);
  };
  add(":root:not(.dark)", declarations(theme.tokens.light));
  add(":root.dark", declarations(theme.tokens.dark));
  add(`:root:not(.dark) ${FRAME_SLOTS}`, declarations(theme.frame?.light));
  add(`:root.dark ${FRAME_SLOTS}`, declarations(theme.frame?.dark));
  return blocks.join("\n");
}

/** The traits attribute value, limited to traits appearance.css implements. */
export function themeTraits(theme: ThemeManifest): string | null {
  const traits = (theme.traits ?? []).filter((trait) =>
    (THEME_TRAITS as readonly string[]).includes(trait),
  );
  return traits.length > 0 ? traits.join(" ") : null;
}

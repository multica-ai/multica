/**
 * Single-skin token derivation for web + desktop.
 *
 * Seeds follow the Linear catalog (accent / foundation / ink). Decorative
 * roles use the same blend coefficients as our appearance solver; text roles
 * are contrast-solved. Output keeps Multica CSS names so Base UI components
 * do not rename. Native mobile is out of scope.
 */

import {
  blend,
  blendHex,
  compositeOver,
  contrastRatio,
  hexToOklchCss,
  hexToRgb,
  luminance,
  rgbToHex,
  shiftLightness,
  solveContrast,
  type Rgb,
} from "./color";

export type ThemeVariant = "light" | "dark";

export type ChromeSeeds = {
  readonly accent: string;
  readonly foundation: string;
  readonly ink: string;
  readonly contrast: number;
  readonly diffAdded: string;
  readonly diffRemoved: string;
};

/** Linear catalog seeds. Contrast is locked; no slider. */
export const PRODUCT_SKIN: Record<ThemeVariant, ChromeSeeds> = {
  light: {
    accent: "#5e6ad2",
    foundation: "#f7f8fa",
    ink: "#2a3140",
    contrast: 45,
    diffAdded: "#00a240",
    diffRemoved: "#ba2623",
  },
  dark: {
    accent: "#5e6ad2",
    foundation: "#17181d",
    ink: "#e6e9ef",
    contrast: 60,
    diffAdded: "#7ad9c0",
    diffRemoved: "#fa423e",
  },
};

const BLACK: Rgb = { red: 0, green: 0, blue: 0 };
const WHITE: Rgb = { red: 255, green: 255, blue: 255 };
const DEFAULT_CONTRAST = { dark: 60, light: 45 } as const;
const SURFACE_UNDER_BASE = { dark: 0.16, light: 0.04 } as const;
const SURFACE_UNDER_SLOPE = { dark: 0.0015, light: 0.0012 } as const;
const PANEL_BASE = { dark: 0.03, light: 0.28 } as const;
const PANEL_SLOPE = { dark: 0.03, light: 0.008 } as const;
const TEXT_CONTRAST_BOOST_SLOPE = 1.5;

const BACKGROUND_TOKEN_KEYS = [
  "--app-shell",
  "--page-canvas",
  "--background",
  "--surface",
  "--surface-raised",
  "--surface-hover",
  "--surface-selected",
  "--card",
  "--popover",
  "--muted",
  "--secondary",
  "--accent",
  "--sidebar",
  "--sidebar-accent",
] as const;

export type TokenMap = Record<string, string>;

export function deriveHexTokens(variant: ThemeVariant): TokenMap {
  const seeds = PRODUCT_SKIN[variant];
  return variant === "light" ? deriveLightHex(seeds) : deriveDarkHex(seeds);
}

export function deriveTokens(variant: ThemeVariant): TokenMap {
  return Object.fromEntries(
    Object.entries(deriveHexTokens(variant)).map(([name, value]) => [
      name,
      value.startsWith("#") ? hexToOklchCss(value) : value,
    ]),
  );
}

function deriveLightHex(seeds: ChromeSeeds): TokenMap {
  const contrast = normalizeContrast(seeds.contrast, "light");
  const foundation = hexToRgb(seeds.foundation);
  const ink = hexToRgb(seeds.ink);
  const elevated = blend(foundation, WHITE, 0.74 + contrast * 0.18);
  const elevatedHex = rgbToHex(elevated);
  const under = blendHex(
    foundation,
    ink,
    SURFACE_UNDER_BASE.light +
      (seeds.contrast - DEFAULT_CONTRAST.light) * SURFACE_UNDER_SLOPE.light,
  );
  const panel = blendHex(
    foundation,
    WHITE,
    PANEL_BASE.light + contrast * PANEL_SLOPE.light,
  );
  const hover = compositeOver(seeds.ink, elevatedHex, 0.04 + contrast * 0.02);
  const selected = compositeOver(seeds.accent, elevatedHex, 0.12 + contrast * 0.04);
  const mutedFill = compositeOver(seeds.ink, elevatedHex, 0.05 + contrast * 0.02);
  const border = compositeOver(seeds.ink, elevatedHex, 0.09 + contrast * 0.04);
  const input = compositeOver(seeds.ink, elevatedHex, 0.12 + contrast * 0.04);

  const surfaces: TokenMap = {
    "--app-shell": under,
    "--page-canvas": elevatedHex,
    "--surface": elevatedHex,
    "--surface-raised": elevatedHex,
    "--surface-hover": hover,
    "--surface-selected": selected,
    "--surface-border": border,
    "--background": elevatedHex,
    "--card": elevatedHex,
    "--popover": elevatedHex,
    "--muted": mutedFill,
    "--secondary": mutedFill,
    "--accent": hover,
    "--sidebar": panel,
    "--sidebar-accent": compositeOver(seeds.ink, panel, 0.06 + contrast * 0.03),
    "--border": border,
    "--input": input,
  };

  const text = solveTextRoles(seeds, surfaces, "light");
  const primary = text["--foreground"]!;
  const brand = solveContrast(seeds.accent, elevatedHex, 4.5);
  const brandHover = shiftLightness(brand, -0.04);
  const brandActive = shiftLightness(brand, -0.07);
  const primaryHover = shiftLightness(primary, -0.05);
  const primaryActive = shiftLightness(primary, -0.08);
  const destructive = solveContrast(seeds.diffRemoved, worstBackground(surfaces), 4.5);
  const warning = solveContrast("#9a6700", worstBackground(surfaces), 4.5);
  const inkHex = seeds.ink;

  return {
    ...surfaces,
    ...text,
    "--surface-foreground": text["--foreground"]!,
    "--surface-selected-foreground": text["--foreground"]!,
    "--card-foreground": text["--foreground"]!,
    "--popover-foreground": text["--foreground"]!,
    "--secondary-foreground": text["--foreground"]!,
    "--accent-foreground": text["--foreground"]!,
    "--sidebar-foreground": text["--foreground"]!,
    "--sidebar-primary": brand,
    "--sidebar-primary-foreground": onFill(brand),
    "--sidebar-accent-foreground": text["--foreground"]!,
    "--sidebar-border": border,
    "--sidebar-ring": brand,
    "--primary": primary,
    "--primary-foreground": onFill(primary),
    "--primary-hover": primaryHover,
    "--primary-pressed": primaryActive,
    "--brand": brand,
    "--brand-foreground": onFill(brand),
    "--brand-hover": brandHover,
    "--brand-pressed": brandActive,
    "--destructive": destructive,
    "--success": seeds.diffAdded,
    "--warning": warning,
    "--info": brand,
    "--ring": compositeOver(seeds.accent, elevatedHex, 0.45),
    "--chart-1": brand,
    "--chart-2": shiftLightness(brand, 0.08),
    "--chart-3": shiftLightness(brand, 0.16),
    "--chart-4": shiftLightness(brand, 0.24),
    "--chart-5": shiftLightness(brand, 0.32),
    "--surface-shadow": shadowPair(inkHex, 0.04, 0.03),
    "--floating-shadow": floatingShadow(inkHex, 0.14, 0.08),
    "--menu-shadow": menuShadow(inkHex, 0.08, 0.05),
    "--scrollbar-thumb": hexToOklchCss("#000000", 0.1),
    "--scrollbar-thumb-hover": hexToOklchCss("#000000", 0.18),
    "--scrollbar-track": "transparent",
    "--find-match": "#e6d35c",
    "--find-match-active": "#e0a84a",
    "--find-match-foreground": "#2a2410",
  };
}

function deriveDarkHex(seeds: ChromeSeeds): TokenMap {
  const contrast = normalizeContrast(seeds.contrast, "dark");
  const foundation = hexToRgb(seeds.foundation);
  const ink = hexToRgb(seeds.ink);
  const elevated = blend(foundation, ink, 0.08 + contrast * 0.08);
  const elevatedHex = rgbToHex(elevated);
  const raised = blendHex(elevated, ink, 0.06 + contrast * 0.04);
  const under = blendHex(
    foundation,
    BLACK,
    SURFACE_UNDER_BASE.dark +
      (seeds.contrast - DEFAULT_CONTRAST.dark) * SURFACE_UNDER_SLOPE.dark,
  );
  const panel = blendHex(
    foundation,
    ink,
    PANEL_BASE.dark + contrast * PANEL_SLOPE.dark,
  );
  const hover = compositeOver(seeds.ink, elevatedHex, 0.06 + contrast * 0.03);
  const selected = compositeOver(seeds.accent, elevatedHex, 0.2 + contrast * 0.05);
  const mutedFill = compositeOver(seeds.ink, elevatedHex, 0.08 + contrast * 0.03);
  const border = compositeOver(seeds.ink, elevatedHex, 0.12 + contrast * 0.06);

  const surfaces: TokenMap = {
    "--app-shell": under,
    "--page-canvas": seeds.foundation,
    "--surface": elevatedHex,
    "--surface-raised": raised,
    "--surface-hover": hover,
    "--surface-selected": selected,
    "--surface-border": border,
    "--background": seeds.foundation,
    "--card": elevatedHex,
    "--popover": raised,
    "--muted": mutedFill,
    "--secondary": mutedFill,
    "--accent": hover,
    "--sidebar": panel,
    "--sidebar-accent": compositeOver(seeds.ink, panel, 0.08 + contrast * 0.04),
    "--border": border,
    "--input": compositeOver(seeds.ink, elevatedHex, 0.16 + contrast * 0.05),
  };

  const text = solveTextRoles(seeds, surfaces, "dark");
  const primary = text["--foreground"]!;
  const brand = solveContrast(seeds.accent, seeds.foundation, 4.5);
  const brandHover = shiftLightness(brand, 0.05);
  const brandActive = shiftLightness(brand, 0.02);
  const primaryHover = shiftLightness(primary, -0.06);
  const primaryActive = shiftLightness(primary, -0.1);
  const destructive = solveContrast(seeds.diffRemoved, lightestBackground(surfaces), 4.5);
  const warning = solveContrast("#ffd60a", lightestBackground(surfaces), 4.5);

  return {
    ...surfaces,
    ...text,
    "--surface-foreground": text["--foreground"]!,
    "--surface-selected-foreground": text["--foreground"]!,
    "--card-foreground": text["--foreground"]!,
    "--popover-foreground": text["--foreground"]!,
    "--secondary-foreground": text["--foreground"]!,
    "--accent-foreground": text["--foreground"]!,
    "--sidebar-foreground": text["--foreground"]!,
    "--sidebar-primary": brand,
    "--sidebar-primary-foreground": onFill(brand),
    "--sidebar-accent-foreground": text["--foreground"]!,
    "--sidebar-border": border,
    "--sidebar-ring": brand,
    "--primary": primary,
    "--primary-foreground": onFill(primary),
    "--primary-hover": primaryHover,
    "--primary-pressed": primaryActive,
    "--brand": brand,
    "--brand-foreground": onFill(brand),
    "--brand-hover": brandHover,
    "--brand-pressed": brandActive,
    "--destructive": destructive,
    "--success": seeds.diffAdded,
    "--warning": warning,
    "--info": brand,
    "--ring": compositeOver(seeds.accent, elevatedHex, 0.55),
    "--chart-1": shiftLightness(brand, 0.1),
    "--chart-2": brand,
    "--chart-3": shiftLightness(brand, -0.08),
    "--chart-4": shiftLightness(brand, -0.16),
    "--chart-5": shiftLightness(brand, -0.24),
    "--surface-shadow": shadowPair("#000000", 0.2, 0.16),
    "--floating-shadow": floatingShadow("#000000", 0.46, 0.28),
    "--menu-shadow": menuShadow("#000000", 0.3, 0.18),
    "--scrollbar-thumb": hexToOklchCss("#ffffff", 0.08),
    "--scrollbar-thumb-hover": hexToOklchCss("#ffffff", 0.18),
    "--scrollbar-track": "transparent",
    "--find-match": "#d4b84a",
    "--find-match-active": "#e08a3c",
    "--find-match-foreground": "#1c160c",
  };
}

function solveTextRoles(
  seeds: ChromeSeeds,
  surfaces: TokenMap,
  variant: ThemeVariant,
): Pick<TokenMap, "--foreground" | "--muted-foreground" | "--faint-foreground"> {
  const boost =
    ((seeds.contrast - DEFAULT_CONTRAST[variant]) / 100) * TEXT_CONTRAST_BOOST_SLOPE;
  const textTarget = 7 + boost * 2;
  const mutedTarget = 4.5 + boost;
  const faintTarget = 3 + boost;
  const constraint =
    variant === "light" ? worstBackground(surfaces) : lightestBackground(surfaces);
  const surface = surfaces["--surface"] ?? seeds.foundation;

  const foreground =
    contrastRatio(seeds.ink, constraint) >= textTarget
      ? seeds.ink
      : solveContrast(seeds.ink, constraint, textTarget);
  const muted = solveContrast(seeds.ink, constraint, mutedTarget);
  let faint = solveContrast(seeds.ink, constraint, faintTarget);

  if (contrastRatio(faint, surface) >= contrastRatio(muted, surface)) {
    faint = shiftLightness(faint, variant === "light" ? 0.04 : -0.04);
    if (contrastRatio(faint, constraint) < 3) {
      faint = solveContrast(seeds.ink, constraint, 3);
    }
  }

  return {
    "--foreground": foreground,
    "--muted-foreground": muted,
    "--faint-foreground": faint,
  };
}

function worstBackground(surfaces: TokenMap): string {
  let worst = surfaces["--surface"] ?? "#ffffff";
  for (const key of BACKGROUND_TOKEN_KEYS) {
    const hex = surfaces[key];
    if (hex === undefined || !hex.startsWith("#")) continue;
    if (luminance(hex) < luminance(worst)) worst = hex;
  }
  return worst;
}

function lightestBackground(surfaces: TokenMap): string {
  let lightest = surfaces["--surface"] ?? "#000000";
  for (const key of BACKGROUND_TOKEN_KEYS) {
    const hex = surfaces[key];
    if (hex === undefined || !hex.startsWith("#")) continue;
    if (luminance(hex) > luminance(lightest)) lightest = hex;
  }
  return lightest;
}

function onFill(fillHex: string): string {
  const white = contrastRatio("#ffffff", fillHex);
  const black = contrastRatio("#111111", fillHex);
  return white >= black ? "#ffffff" : "#17181d";
}

function normalizeContrast(input: number, variant: ThemeVariant): number {
  const defaultContrast = DEFAULT_CONTRAST[variant];
  const base = defaultContrast / 100;
  const adjusted = input / 100 + ((input - defaultContrast) / 60) * 0.7;
  return input <= defaultContrast ? adjusted : base + (adjusted - base) * 2;
}

function shadowPair(hex: string, a: number, b: number): string {
  const { red, green, blue } = hexToRgb(hex);
  return `0 1px 2px rgb(${red} ${green} ${blue} / ${a}), 0 1px 1px rgb(${red} ${green} ${blue} / ${b})`;
}

function floatingShadow(hex: string, a: number, b: number): string {
  const { red, green, blue } = hexToRgb(hex);
  return `0 16px 40px rgb(${red} ${green} ${blue} / ${a}), 0 3px 10px rgb(${red} ${green} ${blue} / ${b})`;
}

function menuShadow(hex: string, a: number, b: number): string {
  const { red, green, blue } = hexToRgb(hex);
  return `0 8px 24px rgb(${red} ${green} ${blue} / ${a}), 0 2px 6px rgb(${red} ${green} ${blue} / ${b})`;
}

export function formatTokenBlock(tokens: TokenMap, indent = "    "): string {
  return Object.keys(tokens)
    .map((name) => `${indent}${name}: ${tokens[name]};`)
    .join("\n");
}

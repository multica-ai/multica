"use client";

import { themeModeVariables, type Theme } from "@multica/core/appearance";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { useT } from "../../i18n";

export type PreviewMode = "light" | "dark";

/** The mode the app is drawn in right now. */
export function useAppliedMode(): PreviewMode {
  const { resolvedTheme } = useTheme();
  return resolvedTheme === "dark" ? "dark" : "light";
}

/** A theme's name; the built-in ones that have a translated name use it. */
export function useThemeLabel() {
  const { t } = useT("settings");
  return (theme: Theme) =>
    theme.id === "default"
      ? t(($) => $.appearance.theme.default)
      : theme.id === "pop"
        ? t(($) => $.appearance.theme.pop)
        : theme.name;
}

type SwatchToken =
  | "app-shell"
  | "page-canvas"
  | "surface"
  | "surface-border"
  | "surface-selected"
  | "foreground"
  | "muted-foreground"
  | "brand"
  | "warning"
  | "success"
  | "info";
type SwatchTokens = Record<SwatchToken, string>;

/**
 * The tokens a theme swatch needs, as tokens.css sets them for the default
 * theme. A theme only overrides some of them, so swatches start from these.
 */
const THEME_SWATCH_BASE: Record<PreviewMode, SwatchTokens> = {
  light: {
    "app-shell": "oklch(0.964 0.001 286)",
    "page-canvas": "oklch(0.988 0 0)",
    surface: "oklch(1 0 0)",
    "surface-border": "oklch(0.92 0.004 286)",
    "surface-selected": "oklch(0.95 0.002 286)",
    foreground: "oklch(0.141 0.005 286)",
    "muted-foreground": "oklch(0.505 0.016 286)",
    brand: "oklch(0.561 0.194 256.4)",
    warning: "oklch(0.75 0.16 85)",
    success: "oklch(0.55 0.16 145)",
    info: "oklch(0.55 0.18 250)",
  },
  dark: {
    "app-shell": "oklch(0.155 0.005 286)",
    "page-canvas": "oklch(0.18 0.005 286)",
    surface: "oklch(0.21 0.006 286)",
    "surface-border": "oklch(0.3 0.006 286)",
    "surface-selected": "oklch(0.3 0.006 286)",
    foreground: "oklch(0.985 0 0)",
    "muted-foreground": "oklch(0.705 0.015 286)",
    brand: "oklch(0.65 0.16 255)",
    warning: "oklch(0.7 0.16 85)",
    success: "oklch(0.65 0.15 145)",
    info: "oklch(0.65 0.18 250)",
  },
};

const swatchCache = new WeakMap<Theme, ReturnType<typeof themeModeVariables>>();

/**
 * Resolves `var()` references a swatch can know without the theme applied:
 * the theme's own variables for the mode and its colour options' defaults.
 */
function resolveSwatchValue(value: string, theme: Theme, variables: Record<string, string>, mode: PreviewMode, depth = 0): string {
  return value.replace(/var\(--([a-z0-9-]+)\)/g, (reference, name: string) => {
    const option = theme.options?.find((o) => o.id === name);
    if (option?.type === "color") return option.default[mode];
    const own = variables[name];
    return own && depth < 4 ? resolveSwatchValue(own, theme, variables, mode, depth + 1) : reference;
  });
}

function swatchTokens(theme: Theme, mode: PreviewMode): SwatchTokens {
  let variables = swatchCache.get(theme);
  if (!variables) {
    variables = themeModeVariables(theme.css);
    swatchCache.set(theme, variables);
  }
  const tokens = { ...THEME_SWATCH_BASE[mode] };
  for (const name of Object.keys(tokens) as (keyof SwatchTokens)[]) {
    const own = variables[mode][name];
    if (own) tokens[name] = resolveSwatchValue(own, theme, variables[mode], mode);
  }
  return tokens;
}

/** A theme in miniature: its frame, page, ink and brand colour. */
export function ThemeSwatch({ theme, mode }: { theme: Theme; mode: PreviewMode }) {
  const tokens = swatchTokens(theme, mode);
  return (
    <span
      aria-hidden="true"
      data-theme-swatch={theme.id}
      data-swatch-mode={mode}
      className="flex h-4 w-6 shrink-0 items-end overflow-hidden rounded-xs p-[2px] ring-1 ring-foreground/10"
      style={{ background: tokens["app-shell"] }}
    >
      <span
        className="flex size-full flex-col justify-center gap-[2px] rounded-[calc(var(--radius-xs)/2)] px-[2px]"
        style={{ background: tokens["page-canvas"], boxShadow: `inset 0 0 0 0.5px ${tokens["surface-border"]}` }}
      >
        <span className="h-[2px] w-3/5 rounded-full" style={{ background: tokens.foreground }} />
        <span className="h-[2px] w-2/5 rounded-full" style={{ background: tokens.brand }} />
      </span>
    </span>
  );
}

/**
 * A theme drawn as a small app window from its own tokens: sidebar, page,
 * a list and the brand button. It is the preview of a theme that has no
 * screenshot: the built-in ones, and the ones imported or in the themes folder.
 */
export function ThemePreview({ theme, mode, className }: { theme: Theme; mode: PreviewMode; className?: string }) {
  const c = swatchTokens(theme, mode);
  const rows = [
    { y: 43, dot: c.warning, width: 44 },
    { y: 55, dot: c.info, width: 32 },
    { y: 67, dot: c.success, width: 38 },
  ];
  return (
    <svg
      viewBox="0 0 160 100"
      preserveAspectRatio="xMidYMin slice"
      aria-hidden="true"
      data-theme-preview={theme.id}
      className={className}
    >
      <rect width="160" height="100" fill={c["app-shell"]} />
      <rect x="8" y="9" width="8" height="8" rx="2" fill={c.foreground} />
      <rect x="5" y="23" width="30" height="8" rx="2" fill={c["surface-selected"]} />
      <rect x="8" y="25" width="20" height="4" rx="2" fill={c.foreground} opacity="0.75" />
      {[18, 23, 14].map((width, i) => (
        <rect key={i} x="8" y={37 + i * 10} width={width} height="4" rx="2" fill={c["muted-foreground"]} opacity="0.55" />
      ))}
      <rect x="40.5" y="8.5" width="140" height="110" rx="7" fill={c["page-canvas"]} stroke={c["surface-border"]} />
      <rect x="50" y="18" width="46" height="5" rx="2.5" fill={c.foreground} />
      <rect x="128" y="16" width="22" height="9" rx="4" fill={c.brand} />
      <rect x="50.5" y="35.5" width="100" height="44" rx="4" fill={c.surface} stroke={c["surface-border"]} />
      {rows.map((row) => (
        <g key={row.y}>
          <circle cx="58" cy={row.y + 2} r="2.5" fill={row.dot} />
          <rect x="65" y={row.y} width={row.width} height="4" rx="2" fill={c.foreground} opacity="0.78" />
          <rect x="130" y={row.y} width="12" height="4" rx="2" fill={c["muted-foreground"]} opacity="0.5" />
        </g>
      ))}
    </svg>
  );
}

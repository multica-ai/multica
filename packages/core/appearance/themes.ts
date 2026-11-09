/**
 * A theme is a folder: `manifest.json` names it and declares its options,
 * `theme.css` overrides the public variables (./theme-variables) under
 * `.theme-light` and `.theme-dark` and may restyle the public anchors
 * (./anchors), and any fonts or images the CSS references sit next to it.
 *
 * Built-in themes are written as token tables so the contrast test can read
 * them, and compiled to the same CSS an outside theme ships. Everything a
 * theme ships goes through `sanitizeThemeCss` (./theme-css).
 * docs/theming.md is the public reference; keep it in step with this file.
 */

/** Bumped only for changes that would break existing themes. */
export const THEME_API_VERSION = 1;

/** Text a theme shows on the Appearance page: one string, or one per locale. */
export type LocalizedText = string | Readonly<Record<string, string>>;

/**
 * Options a theme declares in its manifest, rendered on the Appearance page
 * (the types follow Obsidian's Style Settings). `toggle` and `select` become
 * `data-theme-option-<id>` on <html>; the others set the variable `--<id>`
 * for both modes, or per mode for `color`.
 */
interface ThemeOptionBase {
  id: string;
  label: LocalizedText;
  description?: LocalizedText;
}

/** Starts a group of options. */
export interface ThemeHeadingOption extends ThemeOptionBase {
  type: "heading";
}

/** On: the attribute is present with the value `on`. Off: it is absent. */
export interface ThemeToggleOption extends ThemeOptionBase {
  type: "toggle";
  default: boolean;
}

/** The attribute carries the chosen value. */
export interface ThemeSelectOption extends ThemeOptionBase {
  type: "select";
  default: string;
  choices: readonly { value: string; label: LocalizedText; swatch?: string }[];
}

/** A hex colour per mode. */
export interface ThemeColorOption extends ThemeOptionBase {
  type: "color";
  default: ThemedColor;
}

/** `slider` needs `min` and `max`; the value is written with `unit` appended. */
export interface ThemeNumberOption extends ThemeOptionBase {
  type: "number" | "slider";
  default: number;
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
}

/** Free text: a CSS value for `text`, a font stack for `font`. */
export interface ThemeTextOption extends ThemeOptionBase {
  type: "text" | "font";
  default: string;
}

/** A choice between CSS values. */
export interface ThemeVariableSelectOption extends ThemeOptionBase {
  type: "variable-select";
  default: string;
  choices: readonly { value: string; label: LocalizedText }[];
}

export type ThemeOption =
  | ThemeHeadingOption
  | ThemeToggleOption
  | ThemeSelectOption
  | ThemeColorOption
  | ThemeNumberOption
  | ThemeTextOption
  | ThemeVariableSelectOption;

export interface ThemedColor {
  light: string;
  dark: string;
}

/** manifest.json */
export interface ThemeManifest {
  apiVersion: typeof THEME_API_VERSION;
  id: string;
  name: string;
  /** Who made it; built-in themes name the product they follow. */
  author?: string;
  version?: string;
  description?: string;
  options?: readonly ThemeOption[];
}

/** A theme ready to apply: its manifest, theme.css and the files the CSS references. */
export interface Theme extends ThemeManifest {
  css: string;
  /** Relative path to a base64 `data:` URL. */
  assets?: Readonly<Record<string, string>>;
}

/** Variable name (without `--`) to value. */
export type ThemeTokens = Readonly<Record<string, string>>;

export interface BuiltInTheme extends Theme {
  tokens: { light: ThemeTokens; dark: ThemeTokens };
  /** Tokens for what sits on the frame: the rail, desktop toolbar and tab bar. */
  frame?: { light?: ThemeTokens; dark?: ThemeTokens };
}

const FRAME_ANCHORS = ':is([data-mc="app-rail"], [data-mc="window-toolbar"], [data-mc="tab-bar"])';

function declarations(tokens: ThemeTokens | undefined): string {
  return Object.entries(tokens ?? {})
    .map(([name, value]) => `--${name}: ${value};`)
    .join(" ");
}

/** The CSS a token table stands for. */
export function themeTokensCss(theme: Pick<BuiltInTheme, "tokens" | "frame">): string {
  const blocks: string[] = [];
  const add = (selector: string, body: string) => {
    if (body) blocks.push(`${selector} { ${body} }`);
  };
  add(".theme-light", declarations(theme.tokens.light));
  add(".theme-dark", declarations(theme.tokens.dark));
  add(`.theme-light ${FRAME_ANCHORS}`, declarations(theme.frame?.light));
  add(`.theme-dark ${FRAME_ANCHORS}`, declarations(theme.frame?.dark));
  return blocks.join("\n");
}

function builtInTheme(theme: Omit<BuiltInTheme, "apiVersion" | "css"> & { css?: string }): BuiltInTheme {
  return {
    ...theme,
    apiVersion: THEME_API_VERSION,
    css: [themeTokensCss(theme), theme.css].filter(Boolean).join("\n\n"),
  };
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

const POP_SIGNALS = {
  yellow: POP_YELLOW,
  mint: "oklch(0.87 0.12 165)",
  sky: "oklch(0.83 0.1 230)",
  coral: "oklch(0.8 0.13 30)",
};

const POP_FRAME_ANCHORS = ':is([data-mc="app-rail"], [data-mc="window-toolbar"], [data-mc="tab-bar"])';
const POP_STATUS_BLOCKS: [string, string][] = [
  ['[data-status="backlog"], [data-status="todo"]', "oklch(0.88 0.035 250)"],
  ['[data-status="in_progress"]', "oklch(0.84 0.13 68)"],
  ['[data-status="in_review"]', "oklch(0.86 0.1 165)"],
  ['[data-status="done"]', "oklch(0.8 0.09 255)"],
  ['[data-status="blocked"]', "oklch(0.78 0.13 28)"],
];
const POP_SIGNAL_ON = ':root[data-theme-option-signal]:not([data-theme-option-signal="yellow"])';
const POP_BUTTON = '[data-slot="button"][data-variant="default"]:not(:disabled):hover';
const POP_GROUP = '[data-mc="issue-group-header"]';
const POP_BLOCKED_STATUSES = POP_STATUS_BLOCKS.map(([status]) => status).join(", ");

/** Pop's component treatments, each behind one of its options. */
const POP_CSS = [
  "/* Signal colour: button fill, selection, focus ring and dark floating shadows. */",
  ...Object.entries(POP_SIGNALS)
    .filter(([id]) => id !== "yellow")
    .map(([id, color]) => `:root[data-theme-option-signal="${id}"] { --pop-signal: ${color}; }`),
  `${POP_SIGNAL_ON} { --button-primary: var(--pop-signal); --surface-selected: color-mix(in oklab, var(--pop-signal) 35%, oklch(1 0 0)); }`,
  `${POP_SIGNAL_ON}.theme-dark { --ring: var(--pop-signal); --floating-shadow: 6px 6px 0 var(--pop-signal); --menu-shadow: 4px 4px 0 var(--pop-signal); --surface-selected: color-mix(in oklab, var(--pop-signal) 22%, oklch(0.2 0 0)); }`,
  `${POP_SIGNAL_ON} ${POP_FRAME_ANCHORS} { --sidebar-accent-foreground: var(--pop-signal); --ring: var(--pop-signal); --surface-selected: color-mix(in oklab, var(--pop-signal) 22%, oklch(0.2 0 0)); }`,
  "",
  "/* Primary buttons lift onto a hard shadow on hover instead of darkening. */",
  `:root[data-theme-option-offset-shadow] ${POP_BUTTON} { background-color: var(--button-primary, var(--primary)); translate: -2px -2px; box-shadow: 3px 3px 0 var(--surface-border); }`,
  `@media (prefers-reduced-motion: reduce) { :root[data-theme-option-offset-shadow] ${POP_BUTTON} { translate: none; } }`,
  "",
  "/* Issue group headers become solid blocks per status; custom statuses stay neutral. */",
  `:root[data-theme-option-status-blocks] ${POP_GROUP} { box-shadow: inset 0 0 0 1px var(--surface-border); }`,
  `:root[data-theme-option-status-blocks] ${POP_GROUP}:is(${POP_BLOCKED_STATUSES}) { --foreground: ${INK}; --muted-foreground: oklch(0.3 0 0); color: var(--foreground); }`,
  `:root[data-theme-option-status-blocks] ${POP_GROUP}:is(${POP_BLOCKED_STATUSES}) svg { color: var(--foreground); }`,
  ...POP_STATUS_BLOCKS.map(
    ([status, color]) =>
      `:root[data-theme-option-status-blocks] ${POP_GROUP}:is(${status}) { background-color: ${color}; }`,
  ),
  "",
  "/* Label chips get an ink outline. */",
  `:root[data-theme-option-outlined-chips] [data-mc="label-chip"] { box-shadow: inset 0 0 0 1px var(--surface-border); }`,
].join("\n");

/**
 * Pop: black frame, warm paper canvas, ink outlines, small radius, hard
 * offset shadows and a signal-colour primary. --primary stays ink because it
 * also paints text (`text-primary`); the signal colour reaches buttons
 * through --button-primary.
 */
const POP = builtInTheme({
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
  options: [
    {
      id: "signal",
      type: "select",
      label: {
        en: "Signal color",
        "zh-Hans": "信号色",
        "zh-Hant": "信號色",
        ja: "シグナルカラー",
        ko: "시그널 색상",
        es: "Color de señal",
        fr: "Couleur signal",
      },
      description: {
        en: "Primary buttons, selection and focus.",
        "zh-Hans": "主按钮、选中项和焦点。",
        "zh-Hant": "主按鈕、選取項目和焦點。",
        ja: "主要ボタン、選択項目、フォーカス。",
        ko: "기본 버튼, 선택 항목, 포커스.",
        es: "Botones principales, selección y foco.",
        fr: "Boutons principaux, sélection et focus.",
      },
      default: "yellow",
      choices: [
        { value: "yellow", label: { en: "Yellow", "zh-Hans": "黄", "zh-Hant": "黃", ja: "イエロー", ko: "노랑", es: "Amarillo", fr: "Jaune" }, swatch: POP_SIGNALS.yellow },
        { value: "mint", label: { en: "Mint", "zh-Hans": "薄荷", "zh-Hant": "薄荷", ja: "ミント", ko: "민트", es: "Menta", fr: "Menthe" }, swatch: POP_SIGNALS.mint },
        { value: "sky", label: { en: "Sky", "zh-Hans": "天蓝", "zh-Hant": "天藍", ja: "スカイ", ko: "하늘", es: "Cielo", fr: "Ciel" }, swatch: POP_SIGNALS.sky },
        { value: "coral", label: { en: "Coral", "zh-Hans": "珊瑚", "zh-Hant": "珊瑚", ja: "コーラル", ko: "코랄", es: "Coral", fr: "Corail" }, swatch: POP_SIGNALS.coral },
      ],
    },
    {
      id: "offset-shadow",
      type: "toggle",
      label: { en: "Hard shadows", "zh-Hans": "硬投影", "zh-Hant": "硬投影", ja: "ハードシャドウ", ko: "하드 섀도", es: "Sombras duras", fr: "Ombres franches" },
      description: {
        en: "Primary buttons lift onto a solid offset shadow on hover.",
        "zh-Hans": "悬停时主按钮浮起，露出实色投影。",
        "zh-Hant": "滑過時主按鈕浮起，露出實色投影。",
        ja: "ホバー時に主要ボタンが浮き、ベタの影が出ます。",
        ko: "마우스를 올리면 기본 버튼이 떠오르며 단색 그림자가 생깁니다.",
        es: "Los botones principales se elevan sobre una sombra sólida al pasar el cursor.",
        fr: "Les boutons principaux se soulèvent sur une ombre pleine au survol.",
      },
      default: true,
    },
    {
      id: "status-blocks",
      type: "toggle",
      label: { en: "Status blocks", "zh-Hans": "状态色块", "zh-Hant": "狀態色塊", ja: "ステータスブロック", ko: "상태 블록", es: "Bloques de estado", fr: "Blocs de statut" },
      description: {
        en: "Group headers in issue lists fill with their status color.",
        "zh-Hans": "列表按状态分组时，分组标题整块填色。",
        "zh-Hant": "列表依狀態分組時，分組標題整塊填色。",
        ja: "課題リストのグループ見出しをステータス色で塗ります。",
        ko: "이슈 목록의 그룹 제목을 상태 색으로 채웁니다.",
        es: "Los encabezados de grupo se rellenan con el color de su estado.",
        fr: "Les en-têtes de groupe prennent la couleur de leur statut.",
      },
      default: true,
    },
    {
      id: "outlined-chips",
      type: "toggle",
      label: { en: "Outlined labels", "zh-Hans": "标签描边", "zh-Hant": "標籤描邊", ja: "ラベルの枠線", ko: "라벨 테두리", es: "Etiquetas con contorno", fr: "Étiquettes contourées" },
      description: {
        en: "Label chips get an ink outline.",
        "zh-Hans": "标签加一圈墨线。",
        "zh-Hant": "標籤加一圈墨線。",
        ja: "ラベルに墨色の枠線を付けます。",
        ko: "라벨에 먹색 테두리를 둘러요.",
        es: "Las etiquetas llevan un contorno de tinta.",
        fr: "Les étiquettes reçoivent un contour encre.",
      },
      default: true,
    },
  ],
  css: POP_CSS,
});

/** GitHub (Primer): cool greys, 6px radius, green primary, blue links. */
const GITHUB = builtInTheme({
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
});

/** Blueprint: crisp blue-greys, 4px radius, blue intent for primary and selection. */
const BLUEPRINT = builtInTheme({
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
});

/** Notion: warm greys, quiet hairlines, soft layered shadows, blue primary. */
const NOTION = builtInTheme({
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
});

const DEFAULT = builtInTheme({
  id: DEFAULT_THEME_ID,
  name: "Default",
  tokens: { light: {}, dark: {} },
});

export const BUILT_IN_THEMES: readonly BuiltInTheme[] = [DEFAULT, POP, GITHUB, BLUEPRINT, NOTION];

export function findBuiltInTheme(id: string): BuiltInTheme | undefined {
  return BUILT_IN_THEMES.find((theme) => theme.id === id);
}

const SAFE_VALUE = /^[\w\s#.,%()/+-]+$/;
const UNSAFE_FUNCTION = /\b(url|image|image-set|cross-fade|element|expression|attr|env|src)\s*\(/i;

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

const FONT_VAR = /var\(--[a-z0-9-]+\)/g;
const SAFE_FONT_VALUE = /^[\p{L}\p{N} "',._-]+$/u;

/** A font stack: family names, quoted or not, and var() references to other stacks. */
export function isSafeFontValue(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 300) return false;
  const rest = value.replace(FONT_VAR, "x");
  return SAFE_FONT_VALUE.test(rest) && (rest.split('"').length - 1) % 2 === 0 && (rest.split("'").length - 1) % 2 === 0;
}

const MODE_SELECTOR = /^(?::root|html)?\.theme-(light|dark)$/;

/**
 * The variables a theme sets directly on `.theme-light` and `.theme-dark`,
 * for previews. Only values that would pass as a token are returned.
 */
export function themeModeVariables(css: string): { light: Record<string, string>; dark: Record<string, string> } {
  const out = { light: {} as Record<string, string>, dark: {} as Record<string, string> };
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const rule of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const modes = rule[1]!
      .split(",")
      .map((selector) => MODE_SELECTOR.exec(selector.trim())?.[1] as "light" | "dark" | undefined)
      .filter((mode) => mode !== undefined);
    for (const declaration of rule[2]!.matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+)/gi)) {
      const value = declaration[2]!.trim();
      if (!isSafeThemeValue(value)) continue;
      for (const mode of modes) out[mode][declaration[1]!] = value;
    }
  }
  return out;
}

export const THEME_OPTION_ATTR_PREFIX = "data-theme-option-";

export type ThemeOptionValue = string | boolean | number | Readonly<Partial<ThemedColor>>;
export type ThemeOptionValues = Readonly<Record<string, ThemeOptionValue>>;

export const HEX_COLOR = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i;

/** The value an option has: the person's choice when it is still valid, else the default. */
export function themeOptionValue(option: ThemeOption, chosen: unknown): ThemeOptionValue {
  switch (option.type) {
    case "heading":
      return "";
    case "toggle":
      return typeof chosen === "boolean" ? chosen : option.default;
    case "select":
    case "variable-select":
      return typeof chosen === "string" && option.choices.some((c) => c.value === chosen) ? chosen : option.default;
    case "color": {
      const pick = (mode: keyof ThemedColor) => {
        const value = chosen && typeof chosen === "object" ? (chosen as Partial<ThemedColor>)[mode] : undefined;
        return typeof value === "string" && HEX_COLOR.test(value) ? value : option.default[mode];
      };
      return { light: pick("light"), dark: pick("dark") };
    }
    case "number":
    case "slider": {
      if (typeof chosen !== "number" || !Number.isFinite(chosen)) return option.default;
      return Math.min(option.max ?? Infinity, Math.max(option.min ?? -Infinity, chosen));
    }
    case "text":
      return isSafeThemeValue(chosen) ? chosen : option.default;
    case "font":
      return isSafeFontValue(chosen) ? chosen : option.default;
  }
}

/** The variables the theme's options set, per mode. */
export function themeOptionVariables(
  theme: ThemeManifest,
  chosen: ThemeOptionValues | undefined,
): { light: Record<string, string>; dark: Record<string, string> } {
  const out = { light: {} as Record<string, string>, dark: {} as Record<string, string> };
  for (const option of theme.options ?? []) {
    const name = `--${option.id}`;
    const value = themeOptionValue(option, chosen?.[option.id]);
    if (option.type === "color") {
      const color = value as ThemedColor;
      out.light[name] = color.light;
      out.dark[name] = color.dark;
    } else if (option.type === "number" || option.type === "slider") {
      out.light[name] = out.dark[name] = `${value}${option.unit ?? ""}`;
    } else if (option.type === "text" || option.type === "font" || option.type === "variable-select") {
      out.light[name] = out.dark[name] = value as string;
    }
  }
  return out;
}

/** `data-theme-option-*` attributes for <html>; `null` means absent. */
export function themeOptionAttributes(
  theme: ThemeManifest,
  chosen: ThemeOptionValues | undefined,
): Record<string, string | null> {
  const attributes: Record<string, string | null> = {};
  for (const option of theme.options ?? []) {
    if (option.type !== "toggle" && option.type !== "select") continue;
    const value = themeOptionValue(option, chosen?.[option.id]);
    attributes[`${THEME_OPTION_ATTR_PREFIX}${option.id}`] =
      value === true ? "on" : value === false ? null : String(value);
  }
  return attributes;
}

/** Picks the text for `locale`: exact match, then its language, then English, then any. */
export function localizedText(text: LocalizedText | undefined, locale: string): string {
  if (text === undefined) return "";
  if (typeof text === "string") return text;
  const language = locale.split("-")[0] ?? locale;
  return (
    text[locale] ??
    text[language] ??
    Object.entries(text).find(([key]) => key.startsWith(`${language}-`))?.[1] ??
    text.en ??
    Object.values(text)[0] ??
    ""
  );
}

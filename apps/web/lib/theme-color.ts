const THEME_COLOR_META = "meta[name='theme-color']";

// An unparseable colour leaves `fillStyle` on its previous value rather than
// throwing, so `readPaintedThemeColor` seeds it with something no design token
// would ever be and treats "unchanged" as "could not parse".
const UNPARSEABLE_SENTINEL = "#ff00ff";

/**
 * sRGB renderings of `--page-canvas` (packages/ui/styles/tokens.css), the token
 * `--background` resolves to.
 *
 * Only the server-rendered first paint uses these — HTML ships before any CSS
 * is computed, so the values have to be literals here. Everything after
 * hydration comes from `readPaintedThemeColor`. `theme-color.test.ts` re-derives
 * both from the tokens and fails if they drift apart.
 */
export const THEME_COLOR_FALLBACK = {
  light: "#fbfbfb",
  dark: "#111114",
} as const;

/**
 * The colour the page is painting behind the status bar, as a plain sRGB hex.
 * Returns null when the browser cannot give one.
 *
 * Read from `--background` rather than copied from tokens.css: a copied literal
 * is how this drifted to `#ffffff` while the canvas moved to `--page-canvas`.
 *
 * Tokens are authored in oklch, which `theme-color` parsers are far less
 * consistent about than the CSS engine is — so one pixel gets painted and read
 * back, which is the browser's own conversion, and the meta always carries a
 * hex that every parser has understood for a decade.
 */
export function readPaintedThemeColor(): string | null {
  const background = getComputedStyle(document.documentElement)
    .getPropertyValue("--background")
    .trim();
  if (!background) return null;

  const canvas = document.createElement("canvas");
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return null;

  context.fillStyle = UNPARSEABLE_SENTINEL;
  context.fillStyle = background;
  if (context.fillStyle === UNPARSEABLE_SENTINEL) return null;

  context.fillRect(0, 0, 1, 1);
  const [red = 0, green = 0, blue = 0] = context.getImageData(0, 0, 1, 1).data;
  return `#${[red, green, blue].map(toHexByte).join("")}`;
}

function toHexByte(channel: number): string {
  return channel.toString(16).padStart(2, "0");
}

/**
 * Points `<meta name="theme-color">` at `color`.
 *
 * Any media-scoped variant is dropped. Those exist only as the server's
 * first-paint guess, they follow the OS instead of the in-app theme, and the
 * HTML spec takes the *first* `theme-color` meta whose media matches — so one
 * left in place would shadow this one for good.
 */
export function applyThemeColorMeta(color: string): void {
  const metas = [...document.head.querySelectorAll<HTMLMetaElement>(THEME_COLOR_META)];
  let target = metas.find((meta) => !meta.getAttribute("media"));

  for (const meta of metas) {
    if (meta !== target) meta.remove();
  }

  if (!target) {
    target = document.createElement("meta");
    target.name = "theme-color";
    document.head.appendChild(target);
  }
  target.content = color;
}

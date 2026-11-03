/**
 * Multica brand geometry — brand guidelines v3.1 (MUL-8444).
 *
 * Every mark is built from one arch: straight lines and true circles with a
 * constant stroke. The icon is the wordmark's first letter, scaled up until it
 * no longer fits its tile and gets cropped. Units are the 100×100 icon grid
 * unless stated otherwise; do not tweak these numbers by eye, they are the
 * spec.
 */

const r3 = (n: number) => Math.round(n * 1000) / 1000;

/** Filled outline of one arch: outer width `w`, stroke `t`, legs down to `y1`. */
export function archPath(x0: number, y0: number, y1: number, w: number, t: number): string {
  const R = w / 2;
  const cy = y0 + R;
  const ri = R - t;
  return (
    `M${r3(x0)} ${r3(y1)}V${r3(cy)}A${r3(R)} ${r3(R)} 0 0 1 ${r3(x0 + w)} ${r3(cy)}V${r3(y1)}` +
    `H${r3(x0 + w - t)}V${r3(cy)}A${r3(ri)} ${r3(ri)} 0 0 0 ${r3(x0 + t)} ${r3(cy)}V${r3(y1)}Z`
  );
}

// ---------- icon (F1): stroke 22, arch 68, scaled 0.96, top at 22% ----------
export const ICON_STROKE = 22;
export const ICON_ARCH = 68;
const ICON_SCALE = 0.96;
const ICON_TOP = 22;
const ICON_M_WIDTH = (2 * ICON_ARCH - ICON_STROKE) * ICON_SCALE;
const ICON_LEG = (100 - ICON_TOP) / ICON_SCALE + 4;

/**
 * Places the m on the tile; the m's own paths are in unscaled arch units. The
 * circle (avatar) form sits the m 2 units lower so the arches clear the curve.
 */
export function iconMTransform(shape: "square" | "circle" = "square"): string {
  const top = shape === "circle" ? ICON_TOP + 2 : ICON_TOP;
  return `translate(${r3(50 - ICON_M_WIDTH / 2)} ${top}) scale(${ICON_SCALE})`;
}
export const ICON_TILE_RADIUS = 22.5;

/** The m as two filled arches sharing the middle stem. */
export const ICON_M_PATH =
  archPath(0, 0, ICON_LEG, ICON_ARCH, ICON_STROKE) +
  archPath(ICON_ARCH - ICON_STROKE, 0, ICON_LEG, ICON_ARCH, ICON_STROKE);

/** Centre line of the m, stroked at ICON_STROKE. Drives the "think" highlight. */
export const ICON_M_CENTRE_LINE =
  `M11 ${r3(ICON_LEG)}V34A23 23 0 0 1 57 34V${r3(ICON_LEG)}V34A23 23 0 0 1 103 34V${r3(ICON_LEG)}`;

/**
 * The thin edge in the tile colour that keeps the tile outline intact on any
 * background: 2.6% of the width, never thinner than 1px.
 */
export function iconEdge(sizePx: number): number {
  return Math.max(2.6, 100 / sizePx);
}

// ---------- wordmark (G2): stroke 16, arch 62, x-height 76, ascender 30 ----------
export const WORDMARK_STROKE = 16;

export interface WordmarkLetter {
  d: string;
  dot?: { cx: number; cy: number; r: number };
}

function buildWordmark() {
  const t = WORDMARK_STROKE;
  const W = 62;
  const X = 76;
  const asc = 30;
  const gap = 12;
  const r = (W - t) / 2;
  const a = t / 2;
  const cy1 = a + r;
  const cy2 = X - a - r;
  const c45 = Math.SQRT1_2;
  const A = (x1: number, y1: number) => `A${r3(r)} ${r3(r)} 0 0 1 ${r3(x1)} ${r3(y1)}`;
  const Ar = (x1: number, y1: number) => `A${r3(r)} ${r3(r)} 0 0 0 ${r3(x1)} ${r3(y1)}`;
  const glyphs: Record<string, [number, (x: number) => string, ((x: number) => WordmarkLetter["dot"])?]> = {
    m: [4 * r + t, (x) => `M${r3(x + a)} ${X}V${r3(cy1)}${A(x + a + 2 * r, cy1)}V${X}M${r3(x + a + 2 * r)} ${r3(cy1)}${A(x + a + 4 * r, cy1)}V${X}`],
    u: [2 * r + t, (x) => `M${r3(x + a)} 0V${r3(cy2)}${Ar(x + a + 2 * r, cy2)}M${r3(x + a + 2 * r)} 0V${X}`],
    l: [t, (x) => `M${r3(x + a)} ${-asc}V${X}`],
    t: [2.5 * t, (x) => `M${r3(x + 0.75 * t + a)} ${r3(-asc * 0.66)}V${X}M${r3(x)} ${r3(a)}H${r3(x + 2.5 * t)}`],
    i: [t, (x) => `M${r3(x + a)} 0V${X}`, (x) => ({ cx: r3(x + a), cy: r3(-asc * 0.62), r: r3(t * 0.56) })],
    c: [(W / 2) * (1 + c45), (x) => {
      const cx = x + W / 2;
      return `M${r3(cx + r * c45)} ${r3(cy1 - r * c45)}${Ar(cx - r, cy1)}V${r3(cy2)}${Ar(cx + r * c45, cy2 + r * c45)}`;
    }],
    a: [W, (x) => `M${r3(x + a)} ${r3(cy1)}${A(x + a + 2 * r, cy1)}V${r3(cy2)}${A(x + a, cy2)}ZM${r3(x + a + 2 * r)} 0V${X}`],
  };
  // Optical kerning: the four pairs that read loose at the default spacing.
  const kern: Record<string, number> = { lt: 0.8, ti: 0.85, ic: 0.92, ca: 0.8 };
  const word = "multica";
  const letters: WordmarkLetter[] = [];
  let x = 0;
  for (let k = 0; k < word.length; k++) {
    const [w, d, dot] = glyphs[word[k]!]!;
    letters.push({ d: d(x), dot: dot?.(x) });
    x += w;
    if (k < word.length - 1) x += gap * (kern[word[k]! + word[k + 1]!] ?? 1);
  }
  const top = -asc - 0.5;
  return { letters, viewBox: `0 ${r3(top)} ${r3(x)} ${r3(X - top)}` };
}

export const WORDMARK = buildWordmark();

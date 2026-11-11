/**
 * Multica brand geometry — brand guidelines v5 (MUL-8687).
 *
 * Two shapes. The star: four straight strokes through one centre, 45° apart,
 * eight equal arms with flat ends cut on one circle. The wordmark (G2): drawn
 * centre-lines with a constant stroke, and the star as the dot on its i. Lifted
 * out on its own, that star is the icon. Units are the 100×100 star grid unless
 * stated otherwise; do not tweak these numbers by eye, they are the spec.
 *
 * After changing anything here, run `pnpm generate:brand` (scripts/brand/README.md):
 * the icons, favicons, OG image, README logos, the mobile wordmark and the CLI
 * login page are all generated from this file.
 */

const r3 = (n: number) => Math.round(n * 1000) / 1000;

// ---------- star: arms on the R 50 circle, centre (50, 50) ----------
/** Standalone stroke: lighter than the wordmark's 16, because eight arms pile ink at the centre. */
export const STAR_STROKE = 11;
/** Large surface graphics (bleed, rows) use a thinner stroke. */
export const STAR_GRAPHIC_STROKE = 9;
/** Arm angles in degrees, clockwise from 12 o'clock. */
export const STAR_ARM_ANGLES = [-90, -45, 0, 45, 90, 135, 180, 225] as const;

/**
 * One arm pointing right, from `r0` out to the circle, stroke `t`. Rotate it by
 * an entry of STAR_ARM_ANGLES about (50, 50). Its bounding box starts at the
 * centre, so `transform-origin: 0% 50%` with `transform-box: fill-box` grows it
 * out of the centre.
 */
export function starArmPath(t: number, r0 = 0): string {
  const h = t / 2;
  const x1 = 50 + Math.sqrt(50 * 50 - h * h);
  return `M${r3(50 + r0)} ${r3(50 - h)}H${r3(x1)}A50 50 0 0 1 ${r3(x1)} ${r3(50 + h)}H${r3(50 + r0)}Z`;
}

/**
 * The whole star as one filled path, stroke `t` in grid units. `cx`/`cy`/`R`
 * place it elsewhere at radius R; the stroke scales with it.
 */
export function starPath(t = STAR_STROKE, cx = 50, cy = 50, R = 50): string {
  const k = R / 50;
  const h = (t / 2) * k;
  const along = Math.sqrt(R * R - h * h);
  const pt = (a: number, x: number, y: number) => {
    const c = Math.cos((a * Math.PI) / 180);
    const s = Math.sin((a * Math.PI) / 180);
    return `${r3(cx + x * c - y * s)} ${r3(cy + x * s + y * c)}`;
  };
  return STAR_ARM_ANGLES.map(
    (a) => `M${pt(a, 0, -h)}L${pt(a, along, -h)}A${r3(R)} ${r3(R)} 0 0 1 ${pt(a, along, h)}L${pt(a, 0, h)}Z`,
  ).join("");
}

// ---------- icon: the star on a rounded tile ----------
export const ICON_TILE_RADIUS = 22.5;
/** At 24px and below the star grows and thickens so every arm keeps two pixels. */
export const ICON_SMALL_MAX_PX = 24;

export interface IconStar {
  /** The star's share of the tile width. */
  scale: number;
  /** Stroke in the star's own grid. */
  stroke: number;
}

/** The star inside a tile (`square`) or avatar disc (`circle`) at a rendered size. */
export function iconStar(sizePx: number | undefined, shape: "square" | "circle" = "square"): IconStar {
  const small = sizePx == null || sizePx <= ICON_SMALL_MAX_PX;
  if (shape === "circle") return small ? { scale: 0.64, stroke: 16 } : { scale: 0.56, stroke: 10 };
  return small ? { scale: 0.7, stroke: 16 } : { scale: 0.6, stroke: 10 };
}

/** Centres a star of the given scale on the 100×100 tile. */
export function iconStarTransform(scale: number): string {
  const off = r3(50 - 50 * scale);
  return `translate(${off} ${off}) scale(${scale})`;
}

// ---------- wordmark (G2): stroke 16, arch 62, x-height 76, ascender 30 ----------
export const WORDMARK_STROKE = 16;
/**
 * The star on the i: radius 16, centre 23 above the x-height, so its top clears
 * the l by 9 and its bottom clears the stem by 7. Stroke 18 in its own grid
 * (0.36× the letters) — the standalone 11 would vanish at this size.
 */
export const WORDMARK_STAR = { r: 16, cy: -23, stroke: 18 } as const;

export interface WordmarkLetter {
  /** Centre-line, stroked at WORDMARK_STROKE. */
  d: string;
  /** Filled star path (the i only). */
  star?: string;
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
  const glyphs: Record<string, [number, (x: number) => string, ((x: number) => string)?]> = {
    m: [4 * r + t, (x) => `M${r3(x + a)} ${X}V${r3(cy1)}${A(x + a + 2 * r, cy1)}V${X}M${r3(x + a + 2 * r)} ${r3(cy1)}${A(x + a + 4 * r, cy1)}V${X}`],
    u: [2 * r + t, (x) => `M${r3(x + a)} 0V${r3(cy2)}${Ar(x + a + 2 * r, cy2)}M${r3(x + a + 2 * r)} 0V${X}`],
    l: [t, (x) => `M${r3(x + a)} ${-asc}V${X}`],
    t: [2.5 * t, (x) => `M${r3(x + 0.75 * t + a)} ${r3(-asc * 0.66)}V${X}M${r3(x)} ${r3(a)}H${r3(x + 2.5 * t)}`],
    i: [t, (x) => `M${r3(x + a)} 0V${X}`, (x) => starPath(WORDMARK_STAR.stroke, x + a, WORDMARK_STAR.cy, WORDMARK_STAR.r)],
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
    const [w, d, star] = glyphs[word[k]!]!;
    letters.push({ d: d(x), star: star?.(x) });
    x += w;
    if (k < word.length - 1) x += gap * (kern[word[k]! + word[k + 1]!] ?? 1);
  }
  const top = Math.min(-asc, WORDMARK_STAR.cy - WORDMARK_STAR.r) - 0.5;
  return { letters, viewBox: `0 ${r3(top)} ${r3(x)} ${r3(X - top)}` };
}

export const WORDMARK = buildWordmark();

/**
 * Generated agent avatars (MAKE-291) — character art foundation.
 *
 * The server projects `avatar_url` responses as `gen:<seed>` when the row has
 * a persisted `avatar_seed` and no explicit image (display precedence: image >
 * generated > legacy `emoji:<x>` > placeholder). This module parses that
 * marker and derives the avatar's design deterministically from the seed
 * alone — same seed, same avatar, forever; renaming the agent, reconnecting a
 * provider, or restarting anything cannot change it.
 *
 * Pipeline:
 *
 *   seed --fnv1a32--> hash --trait table--> GeneratedAvatarDesign
 *         --buildAvatarScene--> AvatarScene { defs, shapes }
 *
 * The scene is plain data in a fixed 64×64 space: SVG gradients in `defs`
 * plus layered primitives. The web/desktop renderer
 * (packages/ui/components/common/generated-avatar.tsx) and this platform's
 * renderer (components/ui/generated-avatar.tsx) only translate primitives to
 * their platform's components — identical geometry by construction, locked
 * by identical fixture vectors and scene fingerprints in both test suites.
 * Gradient ids are content-addressed (hash of the gradient payload), so
 * duplicate ids across inline SVGs are always identical definitions —
 * document-wide lookups stay correct with any number of avatars on one page.
 *
 * Art direction (approved reference: collectible chibi companions, bust-only,
 * warm dark-brown line, soft dimensional shading, coordinated outfits):
 * five ORIGINAL archetypes — fox, bear, owl, dragon, android — each with an
 * authored head silhouette, gradient-ramped fur/scales/metal, outfit with
 * collar + utility belt, large expressive eyes (iris gradient + double
 * highlight), and role accessory gear (headphones, goggles, glasses, visor,
 * headset, antenna, book, notebook). All geometry is original artwork drawn
 * for MAKE-291 — no third-party characters or PetDex assets.
 *
 * Stability contract: every visual trait comes from the seed hash. The role
 * suggestion helper (`suggestRoleAccessory`) is intentionally NOT used at
 * render time — it exists so a future creation flow can pick an accessory
 * once from durable metadata (description/instructions, never the name);
 * once chosen, stability comes from the seed like everything else.
 *
 * Pure functions only: no React, no API, no randomness at render time. This
 * is the mobile MIRROR of `packages/ui/lib/avatar-seed.ts` (mobile may only
 * share `@multica/core`, and `packages/ui` may not import core — so there is
 * no package both renderers can legally depend on). The fixture vectors in
 * `avatar-seed.test.ts` are deliberately identical to the web suite's, and
 * both suites pin identical scene fingerprints — if either copy drifts, one
 * of the two suites fails.
 */

const GENERATED_AVATAR_PREFIX = "gen:";

/**
 * Extracts the persisted seed from a `gen:<seed>` display marker. Returns
 * null for every other value (image URL, `emoji:<x>`, empty) so callers can
 * branch without knowing the marker format.
 */
export function parseGeneratedSeed(value?: string | null): string | null {
  if (!value?.startsWith(GENERATED_AVATAR_PREFIX)) return null;
  const seed = value.slice(GENERATED_AVATAR_PREFIX.length).trim();
  return seed || null;
}

/* ------------------------------------------------------------------------- *
 * Archetypes, roles, palettes, color language.
 * ------------------------------------------------------------------------- */

/** The five original character archetypes. */
export type GeneratedAvatarArchetype =
  | "fox"
  | "bear"
  | "owl"
  | "dragon"
  | "android";

const ARCHETYPES: readonly GeneratedAvatarArchetype[] = [
  "fox",
  "bear",
  "owl",
  "dragon",
  "android",
];

/**
 * Role accessory categories. The design's `accessory` trait indexes this
 * list; `suggestRoleAccessory` maps durable agent metadata to one of these
 * categories for creation-time suggestions.
 */
export const ROLE_ACCESSORIES = [
  "headphones",
  "goggles",
  "glasses",
  "visor",
  "headset",
  "antenna",
  "book",
  "notebook",
] as const;

export type RoleAccessory = (typeof ROLE_ACCESSORIES)[number];

/**
 * Three approved illustrated Fox variants. The selection is derived once from
 * the persisted avatar seed, never from mutable agent name, description, or
 * instructions, so existing agent identity cannot drift when metadata changes.
 */
export type FoxVariant = "general" | "developer" | "auditor";
export type BearVariant = "general" | "developer" | "auditor";
export type OwlVariant = "general" | "developer" | "auditor";
export type DragonVariant = "general" | "developer" | "auditor";
export type AndroidVariant = "general" | "developer" | "auditor";

const ILLUSTRATED_VARIANT_LANES = ["general", "developer", "auditor"] as const;

/**
 * Deterministically maps the Fox-only variant lane from the persisted seed.
 * Uses an independently salted FNV-1a hash so variant selection does not alter
 * the existing archetype, palette, expression, or accessory extraction.
 */
export function deriveFoxVariant(seed: string): FoxVariant {
  return (
    ILLUSTRATED_VARIANT_LANES[
      fnv1a32(`fox-variant:${seed}`) % ILLUSTRATED_VARIANT_LANES.length
    ] ?? "general"
  );
}

/**
 * Deterministically maps the Bear-only variant lane from the persisted seed.
 * The Bear namespace is separate from Fox while preserving every existing
 * avatar seed and all original archetype-selection behavior.
 */
export function deriveBearVariant(seed: string): BearVariant {
  return (
    ILLUSTRATED_VARIANT_LANES[
      fnv1a32(`bear-variant:${seed}`) % ILLUSTRATED_VARIANT_LANES.length
    ] ?? "general"
  );
}

/**
 * Deterministically maps the Owl-only variant lane from the persisted seed.
 * Its own namespace preserves archetype hashing and all existing Fox/Bear
 * identity assignments.
 */
export function deriveOwlVariant(seed: string): OwlVariant {
  return (
    ILLUSTRATED_VARIANT_LANES[
      fnv1a32(`owl-variant:${seed}`) % ILLUSTRATED_VARIANT_LANES.length
    ] ?? "general"
  );
}

/**
 * Deterministically maps the Dragon-only variant lane from the persisted seed.
 * Its own namespace preserves archetype hashing and all existing Fox/Bear/Owl
 * identity assignments.
 */
export function deriveDragonVariant(seed: string): DragonVariant {
  return (
    ILLUSTRATED_VARIANT_LANES[
      fnv1a32(`dragon-variant:${seed}`) % ILLUSTRATED_VARIANT_LANES.length
    ] ?? "general"
  );
}

/**
 * Deterministically maps the Android-only variant lane from the persisted seed.
 * Its own namespace preserves archetype hashing and all existing
 * Fox/Bear/Owl/Dragon identity assignments.
 */
export function deriveAndroidVariant(seed: string): AndroidVariant {
  return (
    ILLUSTRATED_VARIANT_LANES[
      fnv1a32(`android-variant:${seed}`) % ILLUSTRATED_VARIANT_LANES.length
    ] ?? "general"
  );
}

/**
 * Variant lane for any illustrated archetype, or null for an archetype with no
 * illustrated family (callers then use the generic procedural renderer).
 */
export function deriveIllustratedVariant(
  archetype: string,
  seed: string,
): "general" | "developer" | "auditor" | null {
  switch (archetype) {
    case "fox":
      return deriveFoxVariant(seed);
    case "bear":
      return deriveBearVariant(seed);
    case "owl":
      return deriveOwlVariant(seed);
    case "dragon":
      return deriveDragonVariant(seed);
    case "android":
      return deriveAndroidVariant(seed);
    default:
      return null;
  }
}

/**
 * Coordinated background palettes. Each entry pairs two hues (gap ≥ 30° so
 * the per-seed jitter can never collapse them); the accent hue rides 165°
 * off hue2 so gadgets (scarf, visor tint, earcup dots) pop against the
 * backdrop while still belonging to the scheme.
 */
const PALETTES = [
  { bg: 18, bg2: 48 }, // sunset
  { bg: 200, bg2: 235 }, // ocean
  { bg: 140, bg2: 170 }, // forest
  { bg: 315, bg2: 345 }, // berry
  { bg: 260, bg2: 290 }, // grape
  { bg: 40, bg2: 70 }, // gold
] as const;

/**
 * Per-archetype authored color language: base with shade and light steps,
 * iris tones, and the coordinated outfit (jacket main, darker step, collar
 * trim). One warm dark-brown outline is shared — the reference set is
 * unified by a single line color, not per-character black.
 */
interface ArchetypeColors {
  base: string;
  dark: string;
  light: string;
  iris: string;
  irisDark: string;
  jacket: string;
  jacketDark: string;
  trim: string;
}

const ARCHETYPE_COLORS: Record<GeneratedAvatarArchetype, ArchetypeColors> = {
  fox: {
    base: "#F0913F",
    dark: "#CE6C1E",
    light: "#FDF0DE",
    iris: "#7A4A22",
    irisDark: "#3E2412",
    jacket: "#303747",
    jacketDark: "#232936",
    trim: "#D9DEE9",
  },
  bear: {
    base: "#B07C50",
    dark: "#8A5B33",
    light: "#EFDBC2",
    iris: "#5C3A1F",
    irisDark: "#2E1B0E",
    jacket: "#35507E",
    jacketDark: "#27405F",
    trim: "#CBD6E8",
  },
  owl: {
    base: "#8A6BC4",
    dark: "#65489F",
    light: "#EAE1F8",
    iris: "#463077",
    irisDark: "#241745",
    jacket: "#3A2E5A",
    jacketDark: "#2C2246",
    trim: "#CBB9EE",
  },
  dragon: {
    // Red/orange fiery dragon per the approved reference (was green — the
    // reference shows warm red-orange scales, orange horns with dark tips).
    base: "#E8603C",
    dark: "#C24526",
    light: "#FBD9C8",
    iris: "#7A2E18",
    irisDark: "#3E1810",
    jacket: "#38322E",
    jacketDark: "#2A2522",
    trim: "#E8CFA8",
  },
  android: {
    base: "#E9EDF4",
    dark: "#B7C0D0",
    light: "#FFFFFF",
    iris: "#4FA8F5",
    irisDark: "#1D5FA8",
    jacket: "#DFE5EE",
    jacketDark: "#394054",
    trim: "#8FA6C4",
  },
};

/** Warm dark brown — one line color across the whole set (reference match). */
const OUTLINE = "#3B2B20";
/** Shared facial ink (noses, mouths, pupils). */
const INK = "#2E2118";

export interface GeneratedAvatarDesign {
  archetype: GeneratedAvatarArchetype;
  /** Index into the coordinated palette table. */
  palette: number;
  /** Background hue pair, 0–359 (palette base ± per-seed jitter). */
  hue: number;
  hue2: number;
  /** Accent hue for scarves/gadgets — complementary to the backdrop. */
  accentHue: number;
  /** Eye style: 0 round, 1 happy arcs, 2 half-lidded, 3 wide sparkle. */
  eyes: 0 | 1 | 2 | 3;
  /** Expression (mouth/beak): 0 smile, 1 open smile, 2 content. */
  expression: 0 | 1 | 2;
  /** Role accessory category index into ROLE_ACCESSORIES. */
  accessory: number;
  /** Background texture: 0 plain, 1 diagonal stripes, 2 dots, 3 rings. */
  pattern: 0 | 1 | 2 | 3;
  /** Facial detail: 0 none, 1 blush, 2 freckles/scale dots. */
  detail: 0 | 1 | 2;
}

/**
 * FNV-1a 32-bit over UTF-16 code units. Deterministic across JS engines
 * (explicit charCodeAt + Math.imul, not the engine-dependent `hashCode`
 * idiom) and dependency-free. Seeds are UUID text in practice, but any
 * string derives cleanly.
 *
 * This hash is the persistence contract: it never changes, so a seed stored
 * today derives the same traits forever.
 */
function fnv1a32(input: string): number {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

/**
 * Derives the full visual design for a seed. Takes ONLY the seed — never the
 * agent's name, provider, or role — so identity is rename-proof by
 * construction. Distinctness comes from silhouette (5 archetypes) × palette
 * (6) × eyes (4) × expression (3) × accessory (8) × pattern (4) × detail (3)
 * plus a ±10° coordinated hue jitter — ~645k combinations, and identity
 * never rests on color alone because the silhouette differs.
 */
export function deriveAvatarDesign(seed: string): GeneratedAvatarDesign {
  const h = fnv1a32(seed);
  const palette = (h >>> 3) % PALETTES.length;
  const jitter = ((h >>> 7) % 21) - 10;
  // Constant-length table — modulo guarantees a hit, but the strict index
  // type still demands the check.
  const base = PALETTES[palette] ?? PALETTES[0];
  const archetype = ARCHETYPES[h % ARCHETYPES.length] ?? "fox";
  const wrap = (v: number) => ((v % 360) + 360) % 360;
  const hue = wrap(base.bg + jitter);
  const hue2 = wrap(base.bg2 + jitter);
  return {
    archetype,
    palette,
    hue,
    hue2,
    accentHue: wrap(hue2 + 165),
    eyes: ((h >>> 12) % 4) as 0 | 1 | 2 | 3,
    expression: ((h >>> 15) % 3) as 0 | 1 | 2,
    accessory: (h >>> 18) % ROLE_ACCESSORIES.length,
    pattern: ((h >>> 21) % 4) as 0 | 1 | 2 | 3,
    detail: ((h >>> 24) % 3) as 0 | 1 | 2,
  };
}

/* ------------------------------------------------------------------------- *
 * Role detection — creation-time suggestion only, never render-time.
 * ------------------------------------------------------------------------- */

/**
 * Suggests a role accessory category from DURABLE agent metadata — the
 * description and instructions. The agent's name is deliberately not a
 * parameter: name-only inference is not reliable role metadata, and names
 * change. Returns a neutral default when metadata is empty or uninformative.
 *
 * Deterministic for a given metadata text. This helper must never run at
 * render time: role text can change later, and a re-render must never change
 * a character. Persisting the chosen accessory at creation is a future
 * phase — until then, seed-derived accessories are the stable source.
 */
export function suggestRoleAccessory(meta: {
  description?: string | null;
  instructions?: string | null;
}): RoleAccessory {
  const text = `${meta.description ?? ""}\n${meta.instructions ?? ""}`.toLowerCase();
  if (!text.trim()) return "headset"; // neutral default

  const category = (
    res: RegExp,
    picks: readonly RoleAccessory[],
  ): RoleAccessory | null => {
    if (!res.test(text)) return null;
    const pick = picks[fnv1a32(text) % picks.length];
    return pick ?? picks[0] ?? "headset";
  };

  return (
    category(/automation|automated|workflow|schedul|\bcron\b|pipeline|trigger|\bbot\b/, ["antenna", "visor"]) ??
    category(/devops|cloud|infra|\bsre\b|deploy|runtime|server|\bops\b|kubernetes|\bk8s\b/, ["visor", "headset"]) ??
    category(/develop|engineer|program|software|\bcode|coding|typescript|golang|python|\bapi\b/, ["headphones", "goggles"]) ??
    category(/audit|oversight|compliance|review|analyst|inspect|quality|\bqa\b|financ/, ["glasses", "notebook"]) ??
    category(/research|investigat|study|scholar|literature|experiment|discover/, ["book", "glasses"]) ??
    category(/manager|project|planner|planning|coordinat|chief of staff|\blead\b|manage/, ["headset", "notebook"]) ??
    "headset"
  );
}

/* ------------------------------------------------------------------------- *
 * Scene model — plain data, 64×64 space, renderer-agnostic.
 * ------------------------------------------------------------------------- */

export interface AvatarGradientStop {
  offset: number;
  color: string;
  opacity?: number;
}

export interface AvatarGradient {
  /** Content-addressed id — identical gradients share ids safely. */
  id: string;
  kind: "linear" | "radial";
  /** Linear endpoints as 0–1 fractions (objectBoundingBox on web). */
  x1?: number;
  y1?: number;
  x2?: number;
  y2?: number;
  /** Radial center/radius as 0–1 fractions. */
  cx?: number;
  cy?: number;
  r?: number;
  stops: AvatarGradientStop[];
}

interface AvatarShapeBase {
  fill?: string;
  stroke?: string;
  strokeWidth?: number;
  opacity?: number;
  strokeLinecap?: "round" | "butt";
  strokeLinejoin?: "round" | "miter";
}

export type AvatarShape =
  | ({ type: "rect"; x: number; y: number; width: number; height: number; rx?: number } & AvatarShapeBase)
  | ({ type: "circle"; cx: number; cy: number; r: number } & AvatarShapeBase)
  | ({ type: "ellipse"; cx: number; cy: number; rx: number; ry: number } & AvatarShapeBase)
  | ({ type: "path"; d: string } & AvatarShapeBase)
  | ({ type: "line"; x1: number; y1: number; x2: number; y2: number } & AvatarShapeBase);

export interface AvatarScene {
  defs: AvatarGradient[];
  shapes: AvatarShape[];
}

/** Content-addressed gradient id: same colors → same id (safe to share). */
function makeGradient(g: Omit<AvatarGradient, "id">): AvatarGradient {
  return { id: "g" + fnv1a32(JSON.stringify(g)).toString(36), ...g };
}

/** `url(#…)` fill/stroke reference for a gradient. */
function gref(g: AvatarGradient): string {
  return `url(#${g.id})`;
}

/* ------------------------------------------------------------------------- *
 * Background texture (seed-stable).
 * ------------------------------------------------------------------------- */

function backgroundPattern(pattern: number): AvatarShape[] {
  if (pattern === 1) {
    return [
      { type: "line", x1: -10, y1: 22, x2: 40, y2: -8, stroke: "#fff", strokeWidth: 6, opacity: 0.08, strokeLinecap: "round" },
      { type: "line", x1: -4, y1: 46, x2: 54, y2: 10, stroke: "#fff", strokeWidth: 6, opacity: 0.08, strokeLinecap: "round" },
      { type: "line", x1: 8, y1: 66, x2: 66, y2: 30, stroke: "#fff", strokeWidth: 6, opacity: 0.08, strokeLinecap: "round" },
      { type: "line", x1: 30, y1: 74, x2: 74, y2: 46, stroke: "#fff", strokeWidth: 6, opacity: 0.08, strokeLinecap: "round" },
    ];
  }
  if (pattern === 2) {
    const dots: AvatarShape[] = (
      [
        [12, 14], [34, 9], [55, 18], [8, 38],
        [30, 32], [54, 42], [16, 58], [44, 56],
      ] as const
    ).map(([cx, cy]) => ({ type: "circle", cx, cy, r: 3, fill: "#fff", opacity: 0.1 }));
    return dots;
  }
  if (pattern === 3) {
    return [
      { type: "circle", cx: 32, cy: 32, r: 12, stroke: "#fff", strokeWidth: 4, opacity: 0.1 },
      { type: "circle", cx: 32, cy: 32, r: 24, stroke: "#fff", strokeWidth: 4, opacity: 0.1 },
      { type: "circle", cx: 32, cy: 32, r: 36, stroke: "#fff", strokeWidth: 4, opacity: 0.1 },
    ];
  }
  return [];
}

/* ------------------------------------------------------------------------- *
 * Scene builder — the art itself. Layer order: background → tail/wing →
 * jacket → neck piece → head → face → eyes → brows → accessory → detail.
 * ------------------------------------------------------------------------- */

/**
 * Builds the full scene for a design. Every shape is drawn in a fixed 64×64
 * coordinate space; the parent avatar container supplies the circular clip
 * (rounded-full), so corners are deliberately painted square here.
 *
 * `accessoryOverride` exists for comparison sheets and tests only — renderers
 * never pass it, so production avatars always use the seed-stable accessory.
 */
export function buildAvatarScene(
  design: GeneratedAvatarDesign,
  options?: { accessoryOverride?: number },
): AvatarScene {
  const { archetype, hue, hue2, accentHue, eyes, expression, pattern, detail } = design;
  const accessory =
    options?.accessoryOverride !== undefined
      ? options.accessoryOverride % ROLE_ACCESSORIES.length
      : design.accessory;
  const c = ARCHETYPE_COLORS[archetype];
  const acc = `hsl(${accentHue}, 72%, 56%)`;
  const accDark = `hsl(${accentHue}, 60%, 40%)`;

  const defs: AvatarGradient[] = [];
  const shapes: AvatarShape[] = [];

  // --- background ---------------------------------------------------------
  const bgGrad = makeGradient({
    kind: "linear",
    x1: 0,
    y1: 0,
    x2: 1,
    y2: 1,
    stops: [
      { offset: 0, color: `hsl(${hue}, 56%, 44%)` },
      { offset: 1, color: `hsl(${hue2}, 60%, 33%)` },
    ],
  });
  defs.push(bgGrad);
  shapes.push({ type: "rect", x: 0, y: 0, width: 64, height: 64, fill: gref(bgGrad) });
  shapes.push(...backgroundPattern(pattern));

  // --- shared gradients ---------------------------------------------------
  // Head ramp: light from upper-left → shade lower-right.
  const headGrad = makeGradient({
    kind: "linear",
    x1: 0.1,
    y1: 0,
    x2: 0.9,
    y2: 1,
    stops: [
      { offset: 0, color: c.base },
      { offset: 0.62, color: c.base },
      { offset: 1, color: c.dark },
    ],
  });
  defs.push(headGrad);

  // Iris depth (radial).
  const irisGrad = makeGradient({
    kind: "radial",
    cx: 0.38,
    cy: 0.34,
    r: 0.85,
    stops: [
      { offset: 0, color: c.iris },
      { offset: 1, color: c.irisDark },
    ],
  });
  defs.push(irisGrad);

  const jacketGrad = makeGradient({
    kind: "linear",
    x1: 0,
    y1: 0,
    x2: 0,
    y2: 1,
    stops: [
      { offset: 0, color: c.jacket },
      { offset: 1, color: c.jacketDark },
    ],
  });
  defs.push(jacketGrad);

  // Accent gadget gradient (scarf, visor, earcups, book, notebook).
  const accGrad = makeGradient({
    kind: "linear",
    x1: 0,
    y1: 0,
    x2: 0,
    y2: 1,
    stops: [
      { offset: 0, color: acc },
      { offset: 1, color: accDark },
    ],
  });
  defs.push(accGrad);

  // --- behind-the-shoulder silhouette pieces ------------------------------
  if (archetype === "fox") {
    // Fluffy tail curling behind the right shoulder, light tip.
    shapes.push({
      type: "path",
      d: "M48,45 C59,42 64,52 59,61 C57,55 53,53 47,55 Z",
      fill: c.base,
      stroke: OUTLINE,
      strokeWidth: 1.7,
      strokeLinejoin: "round",
    });
    shapes.push({ type: "path", d: "M56.5,57.5 C60.5,55.5 63,58 62,61.5 C59.5,62.5 57,61.5 56,59.5 Z", fill: c.light });
  } else if (archetype === "dragon") {
    // Small wing behind the left shoulder — deep crimson so it separates
    // from the red body.
    shapes.push({
      type: "path",
      d: "M14,45 C5,42 1,49 3.5,59 C6.5,52.5 9.5,51 15,52 Z",
      fill: "#A83A20",
      stroke: OUTLINE,
      strokeWidth: 1.7,
      strokeLinejoin: "round",
    });
    shapes.push({ type: "path", d: "M13.5,47.5 C8,47 5,51 4.5,56.5", stroke: c.dark, strokeWidth: 1.4, fill: "none" });
  }

  // --- jacket / outfit ----------------------------------------------------
  shapes.push({
    type: "path",
    d: "M5,64 C5,53 13,46 24,44 L40,44 C51,46 59,53 59,64 Z",
    fill: gref(jacketGrad),
    stroke: OUTLINE,
    strokeWidth: 1.9,
    strokeLinejoin: "round",
  });
  // Shirt V at the neck (owl gets a feather ruff instead).
  if (archetype !== "owl") {
    shapes.push({ type: "path", d: "M27,44 L32,53.5 L37,44 Z", fill: c.trim, opacity: 0.9 });
  }
  // Collar lapels.
  shapes.push({
    type: "path",
    d: "M24,44 L32,54.5 L24.8,57.5 C23.4,52 23,47.5 24,44 Z",
    fill: c.trim,
    stroke: OUTLINE,
    strokeWidth: 1.3,
    strokeLinejoin: "round",
  });
  shapes.push({
    type: "path",
    d: "M40,44 L32,54.5 L39.2,57.5 C40.6,52 41,47.5 40,44 Z",
    fill: c.trim,
    stroke: OUTLINE,
    strokeWidth: 1.3,
    strokeLinejoin: "round",
  });
  // Archetype outfit detailing: dragon armor plating, android chest light.
  if (archetype === "dragon") {
    shapes.push({ type: "line", x1: 41.5, y1: 50, x2: 52, y2: 53.5, stroke: c.trim, strokeWidth: 1.4, opacity: 0.5 });
    shapes.push({ type: "line", x1: 42.5, y1: 55, x2: 53, y2: 58.5, stroke: c.trim, strokeWidth: 1.4, opacity: 0.5 });
  }
  if (archetype === "android") {
    shapes.push({ type: "circle", cx: 32, cy: 58.5, r: 3, fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.2 });
    shapes.push({ type: "circle", cx: 32, cy: 58.5, r: 1.3, fill: "#fff", opacity: 0.85 });
  }
  // Jacket hardware: chest pocket with flap, placket line, buttons.
  shapes.push({ type: "rect", x: 14, y: 49.5, width: 8, height: 7, rx: 1.2, fill: c.jacket, stroke: OUTLINE, strokeWidth: 1.1 });
  shapes.push({ type: "line", x1: 15.2, y1: 51.8, x2: 20.8, y2: 51.8, stroke: c.trim, strokeWidth: 1, opacity: 0.6, strokeLinecap: "round" });
  shapes.push({ type: "line", x1: 32, y1: 54.5, x2: 32, y2: 58.2, stroke: c.trim, strokeWidth: 1, opacity: 0.4, strokeLinecap: "round" });
  shapes.push({ type: "circle", cx: 34.8, cy: 55.8, r: 1.1, fill: c.trim, opacity: 0.85 });
  shapes.push({ type: "circle", cx: 29.2, cy: 57.6, r: 1.1, fill: c.trim, opacity: 0.85 });
  // Utility belt + pouch (shared collectible-gear motif) — high contrast so
  // it reads against the dark jacket even at 32px.
  shapes.push({ type: "rect", x: 8.5, y: 58.5, width: 47, height: 4.4, fill: "#14161E" });
  shapes.push({ type: "line", x1: 9.5, y1: 60.7, x2: 54.5, y2: 60.7, stroke: c.trim, strokeWidth: 0.8, opacity: 0.35, strokeLinecap: "round" });
  shapes.push({ type: "rect", x: 16.5, y: 58, width: 3, height: 5.4, rx: 0.8, fill: c.trim, opacity: 0.7 });
  shapes.push({ type: "rect", x: 44.5, y: 58, width: 3, height: 5.4, rx: 0.8, fill: c.trim, opacity: 0.7 });
  shapes.push({ type: "rect", x: 28, y: 57.5, width: 8, height: 6.5, rx: 1.6, fill: acc, stroke: OUTLINE, strokeWidth: 1.1 });
  shapes.push({ type: "rect", x: 45, y: 57, width: 10, height: 8, rx: 2.4, fill: c.jacketDark, stroke: OUTLINE, strokeWidth: 1.3 });
  shapes.push({ type: "line", x1: 46.5, y1: 60.5, x2: 53.5, y2: 60.5, stroke: OUTLINE, strokeWidth: 1, opacity: 0.6, strokeLinecap: "round" });

  // --- neck piece (fox scarf / owl ruff) ----------------------------------
  if (archetype === "fox") {
    shapes.push({
      type: "path",
      d: "M17.5,42 C24,47.5 40,47.5 46.5,42 L47.5,49.5 C40,55 24,55 16.5,49.5 Z",
      fill: gref(accGrad),
      stroke: OUTLINE,
      strokeWidth: 1.7,
      strokeLinejoin: "round",
    });
    shapes.push({ type: "path", d: "M22,45.5 C27,48.5 37,48.5 42,45.5", stroke: "#fff", strokeWidth: 1.2, opacity: 0.35, fill: "none" });
    shapes.push({ type: "ellipse", cx: 32, cy: 51.5, rx: 5.2, ry: 4.4, fill: accDark, stroke: OUTLINE, strokeWidth: 1.4 });
  } else if (archetype === "owl") {
    // Scalloped feather ruff at the neck.
    shapes.push({
      type: "path",
      d: "M18,43.5 C21,48 25,50 32,50 C39,50 43,48 46,43.5 C47,47.5 45,52.5 40,55 C36,57 28,57 24,55 C19,52.5 17,47.5 18,43.5 Z",
      fill: c.light,
      stroke: OUTLINE,
      strokeWidth: 1.5,
      strokeLinejoin: "round",
    });
  }

  // --- head ---------------------------------------------------------------
  drawHead(shapes, archetype, headGrad);

  // --- face features ------------------------------------------------------
  const eyeL: [number, number] = archetype === "owl" ? [24, 30] : [24.5, 29.5];
  const eyeR: [number, number] = archetype === "owl" ? [40, 30] : [39.5, 29.5];
  drawFace(shapes, archetype, expression, c);
  drawEyes(shapes, eyes, eyeL, eyeR, archetype === "owl", irisGrad, c);
  drawBrows(shapes, archetype, c);

  // --- role accessory -----------------------------------------------------
  drawAccessory(shapes, accessory, archetype, accGrad, acc);

  // --- facial detail ------------------------------------------------------
  if (detail === 1) {
    shapes.push({ type: "ellipse", cx: 18, cy: 37, rx: 4, ry: 2.4, fill: "#F0708A", opacity: 0.5 });
    shapes.push({ type: "ellipse", cx: 46, cy: 37, rx: 4, ry: 2.4, fill: "#F0708A", opacity: 0.5 });
  } else if (detail === 2) {
    const marks: Array<[number, number]> = [
      [17.5, 35.5], [20.5, 38], [46.5, 35.5], [43.5, 38],
    ];
    for (const [cx, cy] of marks) {
      shapes.push({ type: "circle", cx, cy, r: 1.1, fill: c.dark, opacity: 0.8 });
    }
  }

  return { defs, shapes };
}

/* ------------------------------------------------------------------------- *
 * Head silhouettes — authored per archetype.
 * ------------------------------------------------------------------------- */

function drawHead(
  shapes: AvatarShape[],
  archetype: GeneratedAvatarArchetype,
  headGrad: AvatarGradient,
): void {
  const fill = gref(headGrad);
  const stroke = {
    stroke: OUTLINE,
    strokeWidth: 1.9,
    strokeLinejoin: "round" as const,
  };
  const c = ARCHETYPE_COLORS[archetype];

  if (archetype === "fox") {
    // Ears (behind head).
    shapes.push({ type: "path", d: "M12,25 C10,15 10,6.5 11.2,4 C13.5,5.5 22.5,11.5 27.5,17 Z", fill: c.base, ...stroke });
    shapes.push({ type: "path", d: "M52,25 C54,15 54,6.5 52.8,4 C50.5,5.5 41.5,11.5 36.5,17 Z", fill: c.base, ...stroke });
    shapes.push({ type: "path", d: "M14,22.5 C12.8,15.5 12.6,10 12.4,8 C14.5,9.5 20,14 24,17.8 Z", fill: c.light, opacity: 0.95 });
    shapes.push({ type: "path", d: "M50,22.5 C51.2,15.5 51.4,10 51.6,8 C49.5,9.5 44,14 40,17.8 Z", fill: c.light, opacity: 0.95 });
    // Head.
    shapes.push({
      type: "path",
      d: "M12,25 C10,33 11,42 17,46.5 C21,50 26,52 32,52 C38,52 43,50 47,46.5 C53,42 54,33 52,25 C49,16 41,12.5 32,12.5 C23,12.5 15,16 12,25 Z",
      fill,
      ...stroke,
    });
    // Forehead tuft + cheek fluff accents.
    shapes.push({ type: "path", d: "M27.5,14.5 C29.5,10 34.5,10 36.5,14.5 C34.5,12.5 29.5,12.5 27.5,14.5 Z", fill: c.dark });
    shapes.push({ type: "path", d: "M13.5,34 C15,35.5 15.5,37.5 15,39.5", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.7 });
    shapes.push({ type: "path", d: "M50.5,34 C49,35.5 48.5,37.5 49,39.5", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.7 });
    // Rim light upper-left.
    shapes.push({ type: "path", d: "M15,24 C16.5,17.5 23,14 30,13.2", stroke: "#fff", strokeWidth: 2.2, fill: "none", opacity: 0.45, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M49.5,26 C51,33 49.5,40.5 45.5,44.5", stroke: c.dark, strokeWidth: 2.6, fill: "none", opacity: 0.3, strokeLinecap: "round" });
    // Muzzle.
    shapes.push({
      type: "path",
      d: "M23,38.5 C23,33.5 27,31 32,31 C37,31 41,33.5 41,38.5 C41,43.5 37,46.5 32,46.5 C27,46.5 23,43.5 23,38.5 Z",
      fill: c.light,
      stroke: OUTLINE,
      strokeWidth: 1.4,
    });
  } else if (archetype === "bear") {
    shapes.push({ type: "circle", cx: 15.5, cy: 13.5, r: 6.5, fill: c.base, ...stroke });
    shapes.push({ type: "circle", cx: 48.5, cy: 13.5, r: 6.5, fill: c.base, ...stroke });
    shapes.push({ type: "circle", cx: 15.5, cy: 13.5, r: 3.3, fill: c.light });
    shapes.push({ type: "circle", cx: 48.5, cy: 13.5, r: 3.3, fill: c.light });
    shapes.push({
      type: "path",
      d: "M32,10 C43.5,10 52.5,17.5 53,28.5 C53.5,39 45.5,47.5 32,47.5 C18.5,47.5 10.5,39 11,28.5 C11.5,17.5 20.5,10 32,10 Z",
      fill,
      ...stroke,
    });
    shapes.push({ type: "path", d: "M27.5,13 C29.5,9.5 34.5,9.5 36.5,13 C34.5,11.5 29.5,11.5 27.5,13 Z", fill: c.dark });
    shapes.push({ type: "path", d: "M15,21 C17.5,16.5 23,14 28.5,13.4", stroke: "#fff", strokeWidth: 2.2, fill: "none", opacity: 0.45, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M48.5,38 C45.5,43 40,46.2 34,47.2", stroke: c.dark, strokeWidth: 2.6, fill: "none", opacity: 0.3, strokeLinecap: "round" });
    // Fur texture ticks at the head edges.
    shapes.push({ type: "path", d: "M13,30 C14.5,31.5 15,33.5 14.5,35.5", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.6, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M51,30 C49.5,31.5 49,33.5 49.5,35.5", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.6, strokeLinecap: "round" });
    // Muzzle + nose.
    shapes.push({ type: "ellipse", cx: 32, cy: 37.5, rx: 8.5, ry: 6.2, fill: c.light, stroke: OUTLINE, strokeWidth: 1.4 });
    shapes.push({ type: "ellipse", cx: 32, cy: 34.5, rx: 3, ry: 2.2, fill: INK });
  } else if (archetype === "owl") {
    // Feather horn tufts.
    shapes.push({ type: "path", d: "M13.5,18 L10.5,4 L24,11.5 Z", fill: c.base, ...stroke });
    shapes.push({ type: "path", d: "M50.5,18 L53.5,4 L40,11.5 Z", fill: c.base, ...stroke });
    shapes.push({ type: "path", d: "M15.5,16 L14,7.5 L22,11.8 Z", fill: c.light, opacity: 0.9 });
    shapes.push({ type: "path", d: "M48.5,16 L50,7.5 L42,11.8 Z", fill: c.light, opacity: 0.9 });
    shapes.push({
      type: "path",
      d: "M32,9 C44,9 53,17 53.5,29 C54,40.5 45,48.5 32,48.5 C19,48.5 10,40.5 10.5,29 C11,17 20,9 32,9 Z",
      fill,
      ...stroke,
    });
    // Facial disc.
    shapes.push({
      type: "path",
      d: "M32,17 C41.5,17 48,23 48,31.5 C48,40.5 41.5,46 32,46 C22.5,46 16,40.5 16,31.5 C16,23 22.5,17 32,17 Z",
      fill: c.light,
      stroke: OUTLINE,
      strokeWidth: 1.3,
      opacity: 0.97,
    });
    // Feather texture scallops + rim light.
    shapes.push({ type: "path", d: "M13,30 C15,31.5 15.5,34 14.5,36.5", stroke: c.dark, strokeWidth: 1.4, fill: "none", opacity: 0.7 });
    shapes.push({ type: "path", d: "M51,30 C49,31.5 48.5,34 49.5,36.5", stroke: c.dark, strokeWidth: 1.4, fill: "none", opacity: 0.7 });
    shapes.push({ type: "path", d: "M16,20 C20,15.5 25.5,13 31,12.5", stroke: "#fff", strokeWidth: 2.2, fill: "none", opacity: 0.42, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M49.5,33 C49,40 44.5,45 38.5,46.8", stroke: c.dark, strokeWidth: 2.6, fill: "none", opacity: 0.3, strokeLinecap: "round" });
    // More feather scallops low on the sides.
    shapes.push({ type: "path", d: "M16.5,37.5 C18.5,39 19.5,41 19.5,43", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.6, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M47.5,37.5 C45.5,39 44.5,41 44.5,43", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.6, strokeLinecap: "round" });
  } else if (archetype === "dragon") {
    // Horns with dark tips.
    shapes.push({ type: "path", d: "M16.5,16.5 C14,10 14,5.5 15,3.5 C17.5,6 21.5,11 24,15 Z", fill: "#F5A85C", ...stroke });
    shapes.push({ type: "path", d: "M47.5,16.5 C50,10 50,5.5 49,3.5 C46.5,6 42.5,11 40,15 Z", fill: "#F5A85C", ...stroke });
    shapes.push({ type: "path", d: "M15.2,6.6 C15.7,5.4 16.2,4.6 15,3.5 C16.5,4.8 16.9,5.6 17.4,7.1 Z", fill: "#A8551D" });
    shapes.push({ type: "path", d: "M48.8,6.6 C48.3,5.4 47.8,4.6 49,3.5 C47.5,4.8 47.1,5.6 46.6,7.1 Z", fill: "#A8551D" });
    shapes.push({
      type: "path",
      d: "M32,9.5 C43,9.5 51.5,17 52,27.5 C52.5,38.5 45,47.5 32,47.5 C19,47.5 11.5,38.5 12,27.5 C12.5,17 21,9.5 32,9.5 Z",
      fill,
      ...stroke,
    });
    // Forehead scales + rim light.
    shapes.push({ type: "path", d: "M26,16.5 C27.5,14.5 29.5,14.5 31,16.5 C32.5,14.5 34.5,14.5 36,16.5", stroke: c.dark, strokeWidth: 1.5, fill: "none", strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M14.5,21 C18,16 24,13 30,12.5", stroke: "#fff", strokeWidth: 2.2, fill: "none", opacity: 0.45, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M47.5,33 C46.5,39.5 43,44.5 37,46.6", stroke: c.dark, strokeWidth: 2.6, fill: "none", opacity: 0.3, strokeLinecap: "round" });
    // Cheek scale dots.
    for (const [sx, sy] of [[17, 30], [19.5, 33.5], [15.5, 34.5], [47, 30], [44.5, 33.5], [48.5, 34.5]] as const) {
      shapes.push({ type: "circle", cx: sx, cy: sy, r: 1, fill: c.dark, opacity: 0.55 });
    }
    // Snout with nostrils.
    shapes.push({ type: "ellipse", cx: 32, cy: 39, rx: 9, ry: 6.5, fill: c.light, stroke: OUTLINE, strokeWidth: 1.4 });
    shapes.push({ type: "ellipse", cx: 29, cy: 37.5, rx: 1.2, ry: 0.9, fill: INK });
    shapes.push({ type: "ellipse", cx: 35, cy: 37.5, rx: 1.2, ry: 0.9, fill: INK });
  } else {
    // Android — rounded mechanical head + side pods + antenna.
    shapes.push({ type: "line", x1: 32, y1: 10, x2: 32, y2: 4.5, stroke: OUTLINE, strokeWidth: 2, strokeLinecap: "round" });
    shapes.push({ type: "circle", cx: 32, cy: 3.4, r: 2.5, fill: "#4FA8F5", stroke: OUTLINE, strokeWidth: 1.2 });
    shapes.push({ type: "rect", x: 7.5, y: 25, width: 7, height: 14, rx: 3.5, fill: c.jacketDark, ...stroke });
    shapes.push({ type: "rect", x: 49.5, y: 25, width: 7, height: 14, rx: 3.5, fill: c.jacketDark, ...stroke });
    shapes.push({ type: "rect", x: 13.5, y: 10, width: 37, height: 38, rx: 11.5, fill, ...stroke });
    shapes.push({ type: "path", d: "M18,17 C20,14 25,12.5 31,12.5", stroke: "#fff", strokeWidth: 2.2, fill: "none", opacity: 0.75, strokeLinecap: "round" });
    // Cheek fasteners + jaw seam (mechanical material definition).
    shapes.push({ type: "circle", cx: 17.5, cy: 37, r: 1.4, fill: c.dark, opacity: 0.7 });
    shapes.push({ type: "circle", cx: 46.5, cy: 37, r: 1.4, fill: c.dark, opacity: 0.7 });
    shapes.push({ type: "path", d: "M20,43.5 C26,46.2 38,46.2 44,43.5", stroke: c.dark, strokeWidth: 1.3, fill: "none", opacity: 0.65, strokeLinecap: "round" });
    shapes.push({ type: "path", d: "M46,16 C47.5,20 48,25 47.5,30", stroke: "#fff", strokeWidth: 1.6, fill: "none", opacity: 0.55, strokeLinecap: "round" });
  }
}

/* ------------------------------------------------------------------------- *
 * Faces: nose/mouth per archetype, driven by expression.
 * ------------------------------------------------------------------------- */

function drawFace(
  shapes: AvatarShape[],
  archetype: GeneratedAvatarArchetype,
  expression: 0 | 1 | 2,
  c: ArchetypeColors,
): void {
  if (archetype === "owl") {
    // Beak carries the expression.
    if (expression === 1) {
      shapes.push({ type: "path", d: "M32,33.5 L36.5,41 C34.5,43.5 29.5,43.5 27.5,41 Z", fill: "#F2A93B", stroke: OUTLINE, strokeWidth: 1.3, strokeLinejoin: "round" });
      shapes.push({ type: "path", d: "M29.5,40.5 C31,42.5 33,42.5 34.5,40.5 Z", fill: INK, opacity: 0.6 });
    } else if (expression === 2) {
      shapes.push({ type: "path", d: "M32,34 L35.5,40.5 C34,42.3 30,42.3 28.5,40.5 Z", fill: "#F2A93B", stroke: OUTLINE, strokeWidth: 1.3, strokeLinejoin: "round" });
    } else {
      shapes.push({ type: "path", d: "M32,33.5 L36,41 C34.3,43 29.7,43 28,41 Z", fill: "#F2A93B", stroke: OUTLINE, strokeWidth: 1.3, strokeLinejoin: "round" });
      shapes.push({ type: "line", x1: 32, y1: 35, x2: 32, y2: 41.5, stroke: OUTLINE, strokeWidth: 1, opacity: 0.5 });
    }
    return;
  }

  // Nose for the fox (muzzle is drawn by drawHead).
  if (archetype === "fox") {
    shapes.push({ type: "path", d: "M28.6,35 L35.4,35 C36,35 36.3,35.6 35.9,36.1 L32.7,39.8 C32.3,40.3 31.7,40.3 31.3,39.8 L28.1,36.1 C27.7,35.6 28,35 28.6,35 Z", fill: INK });
  } else if (archetype === "android") {
    // Digital mouth line on the face plate.
    shapes.push({ type: "path", d: "M27,41.5 L37,41.5", stroke: c.dark, strokeWidth: 1.6, strokeLinecap: "round" });
  }

  // Mouth under the muzzle/snout (not owl/android).
  if (archetype === "android") return;
  const my = archetype === "dragon" ? 43 : archetype === "fox" ? 43.5 : 40.5;
  if (expression === 1) {
    shapes.push({
      type: "path",
      d: `M27.5,${my - 1} C29,${my + 4.5} 35,${my + 4.5} 36.5,${my - 1} C34,${my + 1.5} 30,${my + 1.5} 27.5,${my - 1} Z`,
      fill: INK,
      strokeLinejoin: "round",
    });
  } else if (expression === 2) {
    shapes.push({
      type: "path",
      d: `M28.5,${my - 0.5} C30,${my + 2.8} 34,${my + 2.8} 35.5,${my - 0.5}`,
      stroke: INK,
      strokeWidth: 1.8,
      fill: "none",
      strokeLinecap: "round",
    });
  } else {
    shapes.push({
      type: "path",
      d: `M28,${my - 1} C29.5,${my + 3.4} 34.5,${my + 3.4} 36,${my - 1}`,
      stroke: INK,
      strokeWidth: 1.9,
      fill: "none",
      strokeLinecap: "round",
    });
  }
}

/* ------------------------------------------------------------------------- *
 * Eyes — large, expressive, reference-style (sclera + iris gradient +
 * pupil + double highlight + upper lid). Owl scales them up.
 * ------------------------------------------------------------------------- */

function drawEyes(
  shapes: AvatarShape[],
  style: 0 | 1 | 2 | 3,
  eyeL: [number, number],
  eyeR: [number, number],
  big: boolean,
  irisGrad: AvatarGradient,
  c: ArchetypeColors,
): void {
  const k = big ? 1.32 : 1;
  const rx = 4.7 * k;
  const ry = 5.3 * k;
  for (const [ex, ey] of [eyeL, eyeR]) {
    if (style === 1) {
      // Happy closed arcs with a lash tick.
      shapes.push({
        type: "path",
        d: `M${ex - rx},${ey + ry * 0.35} Q${ex},${ey - ry * 0.85} ${ex + rx},${ey + ry * 0.35}`,
        stroke: INK,
        strokeWidth: 2.5 * k,
        fill: "none",
        strokeLinecap: "round",
      });
      shapes.push({
        type: "line",
        x1: ex - rx + 0.5,
        y1: ey + ry * 0.75,
        x2: ex - rx + 2 * k,
        y2: ey + ry * 0.55,
        stroke: INK,
        strokeWidth: 1.2 * k,
        strokeLinecap: "round",
      });
      continue;
    }
    // Sclera.
    shapes.push({ type: "ellipse", cx: ex, cy: ey, rx, ry, fill: "#FFF9F0", stroke: OUTLINE, strokeWidth: 1.4 * k });
    // Iris (gradient) — low-center for a friendly gaze.
    const irx = style === 3 ? rx * 0.78 : rx * 0.66;
    const iry = style === 3 ? ry * 0.74 : ry * 0.62;
    shapes.push({ type: "ellipse", cx: ex, cy: ey + ry * 0.08, rx: irx, ry: iry, fill: gref(irisGrad) });
    // Pupil.
    shapes.push({ type: "ellipse", cx: ex, cy: ey + ry * 0.08, rx: irx * 0.45, ry: iry * 0.45, fill: INK, opacity: 0.9 });
    // Signature highlights: large top-left, small bottom-right.
    shapes.push({ type: "circle", cx: ex - irx * 0.4, cy: ey - iry * 0.4, r: irx * 0.34, fill: "#fff" });
    shapes.push({ type: "circle", cx: ex + irx * 0.35, cy: ey + iry * 0.42, r: irx * 0.16, fill: "#fff", opacity: 0.9 });
    if (style === 3) {
      // Wide sparkle adds a soft iris-dark ring.
      shapes.push({ type: "ellipse", cx: ex, cy: ey, rx: rx * 1.12, ry: ry * 1.1, stroke: c.irisDark, strokeWidth: 0.8, fill: "none", opacity: 0.35 });
    }
    // Upper lid line.
    shapes.push({
      type: "path",
      d: `M${ex - rx},${ey - ry * 0.25} C${ex - rx * 0.5},${ey - ry * 1.05} ${ex + rx * 0.5},${ey - ry * 1.05} ${ex + rx},${ey - ry * 0.25}`,
      stroke: INK,
      strokeWidth: 1.7 * k,
      fill: "none",
      strokeLinecap: "round",
    });
    if (style === 2) {
      // Half-lid covering the top of the eye.
      shapes.push({
        type: "path",
        d: `M${ex - rx - 0.5},${ey - ry * 0.45} C${ex - rx * 0.5},${ey - ry * 1.1} ${ex + rx * 0.5},${ey - ry * 1.1} ${ex + rx + 0.5},${ey - ry * 0.45} C${ex + rx * 0.5},${ey - ry * 0.75} ${ex - rx * 0.5},${ey - ry * 0.75} ${ex - rx - 0.5},${ey - ry * 0.45} Z`,
        fill: c.base,
        stroke: OUTLINE,
        strokeWidth: 1,
      });
    }
    // Lower lash accent.
    shapes.push({
      type: "path",
      d: `M${ex - rx * 0.55},${ey + ry * 0.85} Q${ex},${ey + ry * 1.12} ${ex + rx * 0.55},${ey + ry * 0.85}`,
      stroke: INK,
      strokeWidth: 1 * k,
      fill: "none",
      opacity: 0.75,
      strokeLinecap: "round",
    });
  }
}

function drawBrows(
  shapes: AvatarShape[],
  archetype: GeneratedAvatarArchetype,
  c: ArchetypeColors,
): void {
  if (archetype === "android") return;
  if (archetype === "owl") {
    // Prominent angled feather brows — the analytical look.
    shapes.push({ type: "line", x1: 17, y1: 21.5, x2: 27.5, y2: 25, stroke: c.dark, strokeWidth: 2.8, strokeLinecap: "round" });
    shapes.push({ type: "line", x1: 47, y1: 21.5, x2: 36.5, y2: 25, stroke: c.dark, strokeWidth: 2.8, strokeLinecap: "round" });
    return;
  }
  // Soft arcs for furred archetypes.
  shapes.push({ type: "path", d: "M20,23.5 C22,21.5 26,21.5 28,23", stroke: INK, strokeWidth: 1.5, fill: "none", opacity: 0.8, strokeLinecap: "round" });
  shapes.push({ type: "path", d: "M44,23.5 C42,21.5 38,21.5 36,23", stroke: INK, strokeWidth: 1.5, fill: "none", opacity: 0.8, strokeLinecap: "round" });
}

/* ------------------------------------------------------------------------- *
 * Role accessories — meaningful gear, layered over the head/outfit.
 * ------------------------------------------------------------------------- */

function drawAccessory(
  shapes: AvatarShape[],
  accessory: number,
  archetype: GeneratedAvatarArchetype,
  accGrad: AvatarGradient,
  acc: string,
): void {
  const name = ROLE_ACCESSORIES[accessory] ?? "headset";
  const gearDark = "#2B303C";

  switch (name) {
    case "headphones": {
      // Band over the head + two earcups with accent dots.
      shapes.push({ type: "path", d: "M10,29 C10,11 23,4.5 32,4.5 C41,4.5 54,11 54,29", stroke: gearDark, strokeWidth: 4.2, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "path", d: "M12,27 C12.8,13 24,7.5 32,7.5", stroke: "#fff", strokeWidth: 1.3, fill: "none", opacity: 0.35, strokeLinecap: "round" });
      shapes.push({ type: "rect", x: 4.5, y: 25, width: 9.5, height: 14.5, rx: 4.5, fill: gearDark, stroke: OUTLINE, strokeWidth: 1.6 });
      shapes.push({ type: "rect", x: 50, y: 25, width: 9.5, height: 14.5, rx: 4.5, fill: gearDark, stroke: OUTLINE, strokeWidth: 1.6 });
      shapes.push({ type: "circle", cx: 9.25, cy: 32.2, r: 2.1, fill: acc });
      shapes.push({ type: "circle", cx: 54.75, cy: 32.2, r: 2.1, fill: acc });
      break;
    }
    case "goggles": {
      // Pushed-up technical goggles on the forehead.
      shapes.push({ type: "line", x1: 13.5, y1: 18.5, x2: 50.5, y2: 18.5, stroke: gearDark, strokeWidth: 3.4, strokeLinecap: "round" });
      shapes.push({ type: "rect", x: 17, y: 12.5, width: 13, height: 10.5, rx: 5, fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.7 });
      shapes.push({ type: "rect", x: 34, y: 12.5, width: 13, height: 10.5, rx: 5, fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.7 });
      shapes.push({ type: "line", x1: 30, y1: 17, x2: 34, y2: 17, stroke: OUTLINE, strokeWidth: 1.6 });
      shapes.push({ type: "path", d: "M19.5,15.5 C21.5,14 25,13.5 27.5,14", stroke: "#fff", strokeWidth: 1.4, fill: "none", opacity: 0.6, strokeLinecap: "round" });
      shapes.push({ type: "path", d: "M36.5,15.5 C38.5,14 42,13.5 44.5,14", stroke: "#fff", strokeWidth: 1.4, fill: "none", opacity: 0.6, strokeLinecap: "round" });
      break;
    }
    case "glasses": {
      // Round analyst glasses over the eyes.
      const r = archetype === "owl" ? 6.6 : 6.1;
      const lx = archetype === "owl" ? 24 : 24.5;
      const rx2 = archetype === "owl" ? 40 : 39.5;
      const cy = 29.6;
      shapes.push({ type: "circle", cx: lx, cy, r, fill: "#FFFFFF", opacity: 0.12, stroke: OUTLINE, strokeWidth: 1.8 });
      shapes.push({ type: "circle", cx: rx2, cy, r, fill: "#FFFFFF", opacity: 0.12, stroke: OUTLINE, strokeWidth: 1.8 });
      shapes.push({ type: "path", d: `M${lx + r},${cy - 1} C${lx + r + 2.5},${cy - 2.5} ${rx2 - r - 2.5},${cy - 2.5} ${rx2 - r},${cy - 1}`, stroke: OUTLINE, strokeWidth: 1.6, fill: "none" });
      shapes.push({ type: "line", x1: lx - r, y1: cy - 1, x2: 11.5, y2: 26.5, stroke: OUTLINE, strokeWidth: 1.6, strokeLinecap: "round" });
      shapes.push({ type: "line", x1: rx2 + r, y1: cy - 1, x2: 52.5, y2: 26.5, stroke: OUTLINE, strokeWidth: 1.6, strokeLinecap: "round" });
      shapes.push({ type: "path", d: `M${lx - 3},${cy - 3.5} C${lx - 1.5},${cy - 5} ${lx + 1},${cy - 5.5} ${lx + 2.5},${cy - 5}`, stroke: "#fff", strokeWidth: 1.3, fill: "none", opacity: 0.7, strokeLinecap: "round" });
      break;
    }
    case "visor": {
      // Futuristic HUD visor across the eyes (semi-transparent).
      shapes.push({ type: "rect", x: 13, y: 23.5, width: 38, height: 14.5, rx: 7, fill: gref(accGrad), opacity: 0.34, stroke: OUTLINE, strokeWidth: 1.7 });
      shapes.push({ type: "line", x1: 17, y1: 27.5, x2: 31, y2: 27.5, stroke: "#fff", strokeWidth: 1.6, opacity: 0.65, strokeLinecap: "round" });
      shapes.push({ type: "line", x1: 33, y1: 33.5, x2: 46, y2: 33.5, stroke: "#fff", strokeWidth: 1.2, opacity: 0.45, strokeLinecap: "round" });
      shapes.push({ type: "rect", x: 49.5, y: 27, width: 5.5, height: 7, rx: 2, fill: gearDark, stroke: OUTLINE, strokeWidth: 1.3 });
      break;
    }
    case "headset": {
      // Communication headset: one cup + boom mic toward the mouth.
      shapes.push({ type: "path", d: "M9,28 C14,15.5 23,11 32,10.5", stroke: gearDark, strokeWidth: 3.2, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "rect", x: 4, y: 25.5, width: 8.5, height: 13, rx: 4.2, fill: gearDark, stroke: OUTLINE, strokeWidth: 1.6 });
      shapes.push({ type: "circle", cx: 8.25, cy: 32, r: 1.9, fill: acc });
      shapes.push({ type: "path", d: "M7.5,38 C9,45.5 16.5,48 24,45.5", stroke: gearDark, strokeWidth: 2.2, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "circle", cx: 25, cy: 45.2, r: 2.4, fill: acc, stroke: OUTLINE, strokeWidth: 1.2 });
      break;
    }
    case "antenna": {
      if (archetype === "android") {
        // Dual side antennae for the android (it already has a center one).
        shapes.push({ type: "line", x1: 19, y1: 11, x2: 16, y2: 4, stroke: OUTLINE, strokeWidth: 2, strokeLinecap: "round" });
        shapes.push({ type: "circle", cx: 15.5, cy: 3.2, r: 2.2, fill: acc, stroke: OUTLINE, strokeWidth: 1.1 });
        shapes.push({ type: "line", x1: 45, y1: 11, x2: 48, y2: 4, stroke: OUTLINE, strokeWidth: 2, strokeLinecap: "round" });
        shapes.push({ type: "circle", cx: 48.5, cy: 3.2, r: 2.2, fill: acc, stroke: OUTLINE, strokeWidth: 1.1 });
      } else {
        shapes.push({ type: "line", x1: 32, y1: 12.5, x2: 32, y2: 5, stroke: OUTLINE, strokeWidth: 2.2, strokeLinecap: "round" });
        shapes.push({ type: "circle", cx: 32, cy: 3.8, r: 2.7, fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.3 });
        shapes.push({ type: "circle", cx: 31.2, cy: 3, r: 0.9, fill: "#fff", opacity: 0.85 });
      }
      break;
    }
    case "book": {
      // Open book held at chest level (researcher).
      shapes.push({ type: "path", d: "M8,64 C14,58.5 23,58.5 31,62.5 L31,64 Z", fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.5, strokeLinejoin: "round" });
      shapes.push({ type: "path", d: "M56,64 C50,58.5 41,58.5 33,62.5 L33,64 Z", fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.5, strokeLinejoin: "round" });
      shapes.push({ type: "path", d: "M10.5,62.5 C15.5,58.9 22.5,59.2 29.5,62.4", stroke: "#F4EEDF", strokeWidth: 2.4, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "path", d: "M53.5,62.5 C48.5,58.9 41.5,59.2 34.5,62.4", stroke: "#F4EEDF", strokeWidth: 2.4, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "line", x1: 14, y1: 61.5, x2: 24, y2: 61.5, stroke: INK, strokeWidth: 0.9, opacity: 0.5, strokeLinecap: "round" });
      shapes.push({ type: "line", x1: 40, y1: 61.5, x2: 50, y2: 61.5, stroke: INK, strokeWidth: 0.9, opacity: 0.5, strokeLinecap: "round" });
      break;
    }
    case "notebook": {
      // Closed planner/notebook with elastic band (PM/analyst).
      shapes.push({ type: "rect", x: 22, y: 49.5, width: 20, height: 15, rx: 2.5, fill: gref(accGrad), stroke: OUTLINE, strokeWidth: 1.7 });
      shapes.push({ type: "rect", x: 30.5, y: 49.5, width: 3, height: 15, fill: "#20242E", opacity: 0.75 });
      shapes.push({ type: "line", x1: 25.5, y1: 54, x2: 38.5, y2: 54, stroke: "#F4EEDF", strokeWidth: 1.3, opacity: 0.8, strokeLinecap: "round" });
      shapes.push({ type: "line", x1: 25.5, y1: 57.5, x2: 35, y2: 57.5, stroke: "#F4EEDF", strokeWidth: 1.3, opacity: 0.6, strokeLinecap: "round" });
      shapes.push({ type: "path", d: "M24,49.5 C24,47.5 26,46.5 28,46.5", stroke: OUTLINE, strokeWidth: 1.5, fill: "none", strokeLinecap: "round" });
      shapes.push({ type: "path", d: "M38,49.5 C38,47.5 36,46.5 34,46.5", stroke: OUTLINE, strokeWidth: 1.5, fill: "none", strokeLinecap: "round" });
      break;
    }
    default:
      break;
  }
}

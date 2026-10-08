/**
 * Generated agent avatars (MAKE-291) — mobile mirror.
 *
 * Identical algorithm and marker format to `packages/ui/lib/avatar-seed.ts`,
 * which is the canonical web/desktop copy. It is duplicated rather than
 * imported because of the monorepo's dependency rules: mobile may share only
 * `@multica/core`, and `packages/ui` may not import core — so there is no
 * package both renderers can legally depend on. The fixture vectors in
 * `avatar-seed.test.ts` are deliberately identical to the web suite's; if
 * either copy drifts, one of the two suites fails.
 *
 * Pure functions only: no React, no API, no randomness at render time. Same
 * seed, same avatar, forever — renaming the agent or restarting anything
 * cannot change it.
 */

const GENERATED_AVATAR_PREFIX = "gen:";

/**
 * Extracts the persisted seed from a `gen:<seed>` display marker. Returns
 * null for every other value (image URL, `emoji:<x>`, empty).
 */
export function parseGeneratedSeed(value?: string | null): string | null {
  if (!value?.startsWith(GENERATED_AVATAR_PREFIX)) return null;
  const seed = value.slice(GENERATED_AVATAR_PREFIX.length).trim();
  return seed || null;
}

export interface GeneratedAvatarDesign {
  /** Gradient start hue, 0–359. */
  hue: number;
  /** Gradient end hue, 0–359. */
  hue2: number;
  /** Background texture: 0 plain, 1 diagonal stripes, 2 dots, 3 rings. */
  pattern: 0 | 1 | 2 | 3;
  /** Robot face: 0 visor, 1 round eyes, 2 square eyes. */
  face: 0 | 1 | 2;
  /** true → antenna nub on top; false → side ears instead. */
  antenna: boolean;
}

/** FNV-1a 32-bit over UTF-16 code units — deterministic across engines. */
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
 * agent's name, provider, or role — so identity is rename-proof.
 */
export function deriveAvatarDesign(seed: string): GeneratedAvatarDesign {
  const h = fnv1a32(seed);
  const hue = h % 360;
  return {
    hue,
    hue2: (hue + 40 + ((h >>> 9) % 81)) % 360,
    pattern: ((h >>> 3) % 4) as 0 | 1 | 2 | 3,
    face: ((h >>> 5) % 3) as 0 | 1 | 2,
    antenna: ((h >>> 7) & 1) === 1,
  };
}

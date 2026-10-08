/**
 * Generated agent avatars (MAKE-291).
 *
 * The server projects `avatar_url` responses as `gen:<seed>` when the row has
 * a persisted `avatar_seed` and no explicit image (display precedence: image >
 * generated > legacy `emoji:<x>` > placeholder). This module parses that
 * marker and derives the avatar's design deterministically from the seed
 * alone — same seed, same avatar, forever; renaming the agent, reconnecting a
 * provider, or restarting anything cannot change it.
 *
 * Pure functions only: no React, no API, no randomness at render time. The
 * canonical copy for web/desktop lives here; `apps/mobile/lib/avatar-seed.ts`
 * mirrors it (mobile may only share `@multica/core`, and `packages/ui` may not
 * import core) with identical fixture vectors locked in both test suites.
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

/**
 * FNV-1a 32-bit over UTF-16 code units. Deterministic across JS engines
 * (explicit charCodeAt + Math.imul, not the engine-dependent `hashCode`
 * idiom) and dependency-free. Seeds are UUID text in practice, but any
 * string derives cleanly.
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
 * construction. Distinctness comes from hue (360 values) × pattern (4) ×
 * face (2 states) × antenna (2) plus a hue offset that varies per hash.
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

/**
 * Built-in LLM brand faces for agent (and squad) avatars.
 *
 * Stored in `avatar_url` as a marker, same idea as `emoji:🚀`:
 *
 *   brand:claude
 *   brand:claude/gold
 *
 * The optional `/<tier>` suffix picks one of the six capability-grade frames
 * (gold / silver / copper / cyan / diamond / crown) — the same tiers the
 * bundled badge artwork ships, so the ring is part of the picture rather than
 * something the renderers draw. Unknown ids and unknown tiers parse as `null`
 * so a stale marker falls through to the ordinary fallback glyph instead of a
 * broken image.
 */

export const AVATAR_BRAND_PREFIX = "brand:";

/**
 * Exactly the brands the bundled artwork covers — no letter-only placeholders.
 * `composer` is Cursor's Composer model.
 */
export const AVATAR_BRAND_IDS = [
  "gpt",
  "gemini",
  "claude",
  "glm",
  "kimi",
  "deepseek",
  "composer",
  "grok",
  "devin",
] as const;

export type AvatarBrandId = (typeof AVATAR_BRAND_IDS)[number];

export const AVATAR_BRAND_TIERS = [
  "gold",
  "silver",
  "copper",
  "cyan",
  "diamond",
  "crown",
] as const;

export type AvatarBrandTier = (typeof AVATAR_BRAND_TIERS)[number];

export interface AvatarBrand {
  id: AvatarBrandId;
  /** Short label shown in the picker. Brand names stay in English. */
  label: string;
  /** Letter fallback for surfaces that cannot load the artwork (mobile). */
  letter: string;
  /** Tile fill for the letter fallback. Sampled from each brand's badge face. */
  bg: string;
  /** Letter color for the fallback. */
  fg: string;
}

export const AVATAR_BRANDS: readonly AvatarBrand[] = [
  { id: "gpt", label: "GPT", letter: "G", bg: "#111111", fg: "#FFFFFF" },
  { id: "gemini", label: "Gemini", letter: "G", bg: "#1A73E8", fg: "#FFFFFF" },
  { id: "claude", label: "Claude", letter: "C", bg: "#D97757", fg: "#FFFFFF" },
  { id: "glm", label: "GLM", letter: "Z", bg: "#3B6FF6", fg: "#FFFFFF" },
  { id: "kimi", label: "Kimi", letter: "K", bg: "#111111", fg: "#FFFFFF" },
  { id: "deepseek", label: "DeepSeek", letter: "D", bg: "#4D6BFE", fg: "#FFFFFF" },
  { id: "composer", label: "Composer", letter: "C", bg: "#111111", fg: "#FFFFFF" },
  { id: "grok", label: "Grok", letter: "G", bg: "#0A0A0A", fg: "#F4F4F5" },
  { id: "devin", label: "Devin", letter: "D", bg: "#0E7490", fg: "#FFFFFF" },
];

export const AVATAR_BRAND_BY_ID: Record<AvatarBrandId, AvatarBrand> =
  Object.fromEntries(AVATAR_BRANDS.map((brand) => [brand.id, brand])) as Record<
    AvatarBrandId,
    AvatarBrand
  >;

/**
 * Capability-tier colors, sampled from the badge frames themselves. Used only
 * where the artwork cannot load: the mobile letter fallback draws this color
 * as the ring border, and the picker swatch previews the tier.
 */
export const AVATAR_BRAND_TIER_COLOR: Record<AvatarBrandTier, string> = {
  gold: "#D7A414",
  silver: "#B9C0CA",
  copper: "#C7793B",
  cyan: "#10BDC1",
  diamond: "#4D9DFF",
  crown: "#E0AD23",
};

export interface ParsedAvatarBrand {
  id: AvatarBrandId;
  tier: AvatarBrandTier | null;
}

const BRAND_ID_BY_VALUE: Record<string, true> = Object.fromEntries(
  AVATAR_BRAND_IDS.map((id) => [id, true as const]),
);

const TIER_BY_VALUE: Record<string, true> = Object.fromEntries(
  AVATAR_BRAND_TIERS.map((tier) => [tier, true as const]),
);

export function isAvatarBrandId(value: string): value is AvatarBrandId {
  return value in BRAND_ID_BY_VALUE;
}

export function isAvatarBrandTier(value: string): value is AvatarBrandTier {
  return value in TIER_BY_VALUE;
}

export function parseAvatarBrand(
  value?: string | null,
): ParsedAvatarBrand | null {
  if (!value?.startsWith(AVATAR_BRAND_PREFIX)) return null;

  const rest = value.slice(AVATAR_BRAND_PREFIX.length).trim();
  if (!rest) return null;

  const slash = rest.indexOf("/");
  const id = slash === -1 ? rest : rest.slice(0, slash);
  const tierRaw = slash === -1 ? "" : rest.slice(slash + 1);
  if (!isAvatarBrandId(id)) return null;
  if (tierRaw && !isAvatarBrandTier(tierRaw)) return null;

  return { id, tier: tierRaw ? (tierRaw as AvatarBrandTier) : null };
}

export function formatAvatarBrand(
  id: AvatarBrandId,
  tier: AvatarBrandTier | null = null,
): string {
  return tier
    ? `${AVATAR_BRAND_PREFIX}${id}/${tier}`
    : `${AVATAR_BRAND_PREFIX}${id}`;
}

/** Bundled artwork filename for a brand face, tiered or frameless. */
export function avatarBrandAssetName(
  id: AvatarBrandId,
  tier: AvatarBrandTier | null,
): string {
  return tier ? `${id}-${tier}.webp` : `${id}.webp`;
}

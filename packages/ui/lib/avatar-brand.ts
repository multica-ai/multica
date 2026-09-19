/**
 * Built-in LLM brand faces for agent (and squad) avatars.
 *
 * Stored in `avatar_url` as a marker, same idea as `emoji:🚀`:
 *
 *   brand:claude
 *   brand:claude/flagship
 *
 * The optional `/<ring>` suffix is a capability-grade halo drawn around the
 * face. Unknown ids and unknown rings parse as `null` so a stale marker
 * falls through to the ordinary fallback glyph instead of a broken image.
 */

export const AVATAR_BRAND_PREFIX = "brand:";

export const AVATAR_BRAND_IDS = [
  "grok",
  "gpt",
  "claude",
  "gemini",
  "deepseek",
  "kimi",
  "glm",
  "qwen",
  "llama",
  "mistral",
  "devin",
  "cursor",
  "copilot",
  "opencode",
] as const;

export type AvatarBrandId = (typeof AVATAR_BRAND_IDS)[number];

export const AVATAR_BRAND_RINGS = ["flagship", "standard", "fast"] as const;

export type AvatarBrandRing = (typeof AVATAR_BRAND_RINGS)[number];

export interface AvatarBrand {
  id: AvatarBrandId;
  /** Short label shown in the picker. Brand names stay in English. */
  label: string;
  /** Tile fill. */
  bg: string;
  /** Glyph / letter color. */
  fg: string;
  /** One-letter fallback for surfaces that cannot draw the SVG mark. */
  letter: string;
}

export const AVATAR_BRANDS: readonly AvatarBrand[] = [
  { id: "grok", label: "Grok", bg: "#0A0A0A", fg: "#F4F4F5", letter: "G" },
  { id: "gpt", label: "GPT", bg: "#10A37F", fg: "#FFFFFF", letter: "G" },
  { id: "claude", label: "Claude", bg: "#D97757", fg: "#FFFFFF", letter: "C" },
  { id: "gemini", label: "Gemini", bg: "#1A73E8", fg: "#FFFFFF", letter: "G" },
  { id: "deepseek", label: "DeepSeek", bg: "#4D6BFE", fg: "#FFFFFF", letter: "D" },
  { id: "kimi", label: "Kimi", bg: "#1F1147", fg: "#FFFFFF", letter: "K" },
  { id: "glm", label: "GLM", bg: "#1A56DB", fg: "#FFFFFF", letter: "Z" },
  { id: "qwen", label: "Qwen", bg: "#6A3DE8", fg: "#FFFFFF", letter: "Q" },
  { id: "llama", label: "Llama", bg: "#12101A", fg: "#ED9D3C", letter: "L" },
  { id: "mistral", label: "Mistral", bg: "#FA520F", fg: "#FFFFFF", letter: "M" },
  { id: "devin", label: "Devin", bg: "#0B1220", fg: "#5EEAD4", letter: "D" },
  { id: "cursor", label: "Cursor", bg: "#0A0A0A", fg: "#F4F4F5", letter: "C" },
  { id: "copilot", label: "Copilot", bg: "#0D1117", fg: "#F0F6FC", letter: "C" },
  { id: "opencode", label: "OpenCode", bg: "#3F3F46", fg: "#E4E4E7", letter: "O" },
];

export const AVATAR_BRAND_BY_ID: Record<AvatarBrandId, AvatarBrand> =
  Object.fromEntries(AVATAR_BRANDS.map((brand) => [brand.id, brand])) as Record<
    AvatarBrandId,
    AvatarBrand
  >;

/**
 * Halo colors. Flagship is the cyan ring from the motivating design;
 * the other two grades sit next to it as cooler / warmer companions
 * rather than a traffic-light.
 */
export const AVATAR_BRAND_RING_COLOR: Record<AvatarBrandRing, string> = {
  flagship: "#22D3EE",
  standard: "#A78BFA",
  fast: "#34D399",
};

export interface ParsedAvatarBrand {
  id: AvatarBrandId;
  ring: AvatarBrandRing | null;
}

const BRAND_ID_SET = new Set<string>(AVATAR_BRAND_IDS);
const RING_SET = new Set<string>(AVATAR_BRAND_RINGS);

export function isAvatarBrandId(value: string): value is AvatarBrandId {
  return BRAND_ID_SET.has(value);
}

export function isAvatarBrandRing(value: string): value is AvatarBrandRing {
  return RING_SET.has(value);
}

export function parseAvatarBrand(
  value?: string | null,
): ParsedAvatarBrand | null {
  if (!value?.startsWith(AVATAR_BRAND_PREFIX)) return null;

  const rest = value.slice(AVATAR_BRAND_PREFIX.length).trim();
  if (!rest) return null;

  const slash = rest.indexOf("/");
  const id = slash === -1 ? rest : rest.slice(0, slash);
  const ringRaw = slash === -1 ? "" : rest.slice(slash + 1);
  if (!isAvatarBrandId(id)) return null;
  if (ringRaw && !isAvatarBrandRing(ringRaw)) return null;

  return { id, ring: ringRaw ? (ringRaw as AvatarBrandRing) : null };
}

export function formatAvatarBrand(
  id: AvatarBrandId,
  ring: AvatarBrandRing | null = null,
): string {
  return ring ? `${AVATAR_BRAND_PREFIX}${id}/${ring}` : `${AVATAR_BRAND_PREFIX}${id}`;
}

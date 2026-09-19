import type { AvatarBrandId, AvatarBrandTier } from "@multica/ui/lib/avatar-brand";

/**
 * Bundled brand badge artwork — 9 LLM brands, one frameless face plus six
 * capability-tier frames each, 256×256 lossless webp on transparency
 * (`brand-avatar-assets/`). The tier frame is part of the picture; renderers
 * never draw the ring themselves.
 *
 * Static imports, not import.meta.glob: apps/web compiles this package through
 * Next.js, which has no Vite glob — the same reason packages/views uses plain
 * static asset imports (provider-logo.tsx). Next.js exposes them as
 * `StaticImageData` objects while Vite/electron-vite expose URL strings; the
 * union mirrors `packages/views/assets.d.ts`. Normalize with `brandAssetSrc`.
 */

import asset_claude_copper from "./brand-avatar-assets/claude-copper.webp";
import asset_claude_crown from "./brand-avatar-assets/claude-crown.webp";
import asset_claude_cyan from "./brand-avatar-assets/claude-cyan.webp";
import asset_claude_diamond from "./brand-avatar-assets/claude-diamond.webp";
import asset_claude_gold from "./brand-avatar-assets/claude-gold.webp";
import asset_claude_silver from "./brand-avatar-assets/claude-silver.webp";
import asset_claude from "./brand-avatar-assets/claude.webp";
import asset_composer_copper from "./brand-avatar-assets/composer-copper.webp";
import asset_composer_crown from "./brand-avatar-assets/composer-crown.webp";
import asset_composer_cyan from "./brand-avatar-assets/composer-cyan.webp";
import asset_composer_diamond from "./brand-avatar-assets/composer-diamond.webp";
import asset_composer_gold from "./brand-avatar-assets/composer-gold.webp";
import asset_composer_silver from "./brand-avatar-assets/composer-silver.webp";
import asset_composer from "./brand-avatar-assets/composer.webp";
import asset_deepseek_copper from "./brand-avatar-assets/deepseek-copper.webp";
import asset_deepseek_crown from "./brand-avatar-assets/deepseek-crown.webp";
import asset_deepseek_cyan from "./brand-avatar-assets/deepseek-cyan.webp";
import asset_deepseek_diamond from "./brand-avatar-assets/deepseek-diamond.webp";
import asset_deepseek_gold from "./brand-avatar-assets/deepseek-gold.webp";
import asset_deepseek_silver from "./brand-avatar-assets/deepseek-silver.webp";
import asset_deepseek from "./brand-avatar-assets/deepseek.webp";
import asset_devin_copper from "./brand-avatar-assets/devin-copper.webp";
import asset_devin_crown from "./brand-avatar-assets/devin-crown.webp";
import asset_devin_cyan from "./brand-avatar-assets/devin-cyan.webp";
import asset_devin_diamond from "./brand-avatar-assets/devin-diamond.webp";
import asset_devin_gold from "./brand-avatar-assets/devin-gold.webp";
import asset_devin_silver from "./brand-avatar-assets/devin-silver.webp";
import asset_devin from "./brand-avatar-assets/devin.webp";
import asset_gemini_copper from "./brand-avatar-assets/gemini-copper.webp";
import asset_gemini_crown from "./brand-avatar-assets/gemini-crown.webp";
import asset_gemini_cyan from "./brand-avatar-assets/gemini-cyan.webp";
import asset_gemini_diamond from "./brand-avatar-assets/gemini-diamond.webp";
import asset_gemini_gold from "./brand-avatar-assets/gemini-gold.webp";
import asset_gemini_silver from "./brand-avatar-assets/gemini-silver.webp";
import asset_gemini from "./brand-avatar-assets/gemini.webp";
import asset_glm_copper from "./brand-avatar-assets/glm-copper.webp";
import asset_glm_crown from "./brand-avatar-assets/glm-crown.webp";
import asset_glm_cyan from "./brand-avatar-assets/glm-cyan.webp";
import asset_glm_diamond from "./brand-avatar-assets/glm-diamond.webp";
import asset_glm_gold from "./brand-avatar-assets/glm-gold.webp";
import asset_glm_silver from "./brand-avatar-assets/glm-silver.webp";
import asset_glm from "./brand-avatar-assets/glm.webp";
import asset_gpt_copper from "./brand-avatar-assets/gpt-copper.webp";
import asset_gpt_crown from "./brand-avatar-assets/gpt-crown.webp";
import asset_gpt_cyan from "./brand-avatar-assets/gpt-cyan.webp";
import asset_gpt_diamond from "./brand-avatar-assets/gpt-diamond.webp";
import asset_gpt_gold from "./brand-avatar-assets/gpt-gold.webp";
import asset_gpt_silver from "./brand-avatar-assets/gpt-silver.webp";
import asset_gpt from "./brand-avatar-assets/gpt.webp";
import asset_grok_copper from "./brand-avatar-assets/grok-copper.webp";
import asset_grok_crown from "./brand-avatar-assets/grok-crown.webp";
import asset_grok_cyan from "./brand-avatar-assets/grok-cyan.webp";
import asset_grok_diamond from "./brand-avatar-assets/grok-diamond.webp";
import asset_grok_gold from "./brand-avatar-assets/grok-gold.webp";
import asset_grok_silver from "./brand-avatar-assets/grok-silver.webp";
import asset_grok from "./brand-avatar-assets/grok.webp";
import asset_kimi_copper from "./brand-avatar-assets/kimi-copper.webp";
import asset_kimi_crown from "./brand-avatar-assets/kimi-crown.webp";
import asset_kimi_cyan from "./brand-avatar-assets/kimi-cyan.webp";
import asset_kimi_diamond from "./brand-avatar-assets/kimi-diamond.webp";
import asset_kimi_gold from "./brand-avatar-assets/kimi-gold.webp";
import asset_kimi_silver from "./brand-avatar-assets/kimi-silver.webp";
import asset_kimi from "./brand-avatar-assets/kimi.webp";

interface StaticImageAsset {
  src: string;
  height?: number;
  width?: number;
  blurDataURL?: string;
}

type BrandAssetModule = string | StaticImageAsset;

const ASSET_BY_NAME: Record<string, BrandAssetModule> = {
  "claude-copper.webp": asset_claude_copper,
  "claude-crown.webp": asset_claude_crown,
  "claude-cyan.webp": asset_claude_cyan,
  "claude-diamond.webp": asset_claude_diamond,
  "claude-gold.webp": asset_claude_gold,
  "claude-silver.webp": asset_claude_silver,
  "claude.webp": asset_claude,
  "composer-copper.webp": asset_composer_copper,
  "composer-crown.webp": asset_composer_crown,
  "composer-cyan.webp": asset_composer_cyan,
  "composer-diamond.webp": asset_composer_diamond,
  "composer-gold.webp": asset_composer_gold,
  "composer-silver.webp": asset_composer_silver,
  "composer.webp": asset_composer,
  "deepseek-copper.webp": asset_deepseek_copper,
  "deepseek-crown.webp": asset_deepseek_crown,
  "deepseek-cyan.webp": asset_deepseek_cyan,
  "deepseek-diamond.webp": asset_deepseek_diamond,
  "deepseek-gold.webp": asset_deepseek_gold,
  "deepseek-silver.webp": asset_deepseek_silver,
  "deepseek.webp": asset_deepseek,
  "devin-copper.webp": asset_devin_copper,
  "devin-crown.webp": asset_devin_crown,
  "devin-cyan.webp": asset_devin_cyan,
  "devin-diamond.webp": asset_devin_diamond,
  "devin-gold.webp": asset_devin_gold,
  "devin-silver.webp": asset_devin_silver,
  "devin.webp": asset_devin,
  "gemini-copper.webp": asset_gemini_copper,
  "gemini-crown.webp": asset_gemini_crown,
  "gemini-cyan.webp": asset_gemini_cyan,
  "gemini-diamond.webp": asset_gemini_diamond,
  "gemini-gold.webp": asset_gemini_gold,
  "gemini-silver.webp": asset_gemini_silver,
  "gemini.webp": asset_gemini,
  "glm-copper.webp": asset_glm_copper,
  "glm-crown.webp": asset_glm_crown,
  "glm-cyan.webp": asset_glm_cyan,
  "glm-diamond.webp": asset_glm_diamond,
  "glm-gold.webp": asset_glm_gold,
  "glm-silver.webp": asset_glm_silver,
  "glm.webp": asset_glm,
  "gpt-copper.webp": asset_gpt_copper,
  "gpt-crown.webp": asset_gpt_crown,
  "gpt-cyan.webp": asset_gpt_cyan,
  "gpt-diamond.webp": asset_gpt_diamond,
  "gpt-gold.webp": asset_gpt_gold,
  "gpt-silver.webp": asset_gpt_silver,
  "gpt.webp": asset_gpt,
  "grok-copper.webp": asset_grok_copper,
  "grok-crown.webp": asset_grok_crown,
  "grok-cyan.webp": asset_grok_cyan,
  "grok-diamond.webp": asset_grok_diamond,
  "grok-gold.webp": asset_grok_gold,
  "grok-silver.webp": asset_grok_silver,
  "grok.webp": asset_grok,
  "kimi-copper.webp": asset_kimi_copper,
  "kimi-crown.webp": asset_kimi_crown,
  "kimi-cyan.webp": asset_kimi_cyan,
  "kimi-diamond.webp": asset_kimi_diamond,
  "kimi-gold.webp": asset_kimi_gold,
  "kimi-silver.webp": asset_kimi_silver,
  "kimi.webp": asset_kimi,
};

/** Normalize a static import to a usable URL across Next.js and Vite. */
function brandAssetSrc(mod: BrandAssetModule): string {
  return typeof mod === "string" ? mod : mod.src;
}

/**
 * Resolves the artwork URL for a brand face. `tier === null` picks the
 * frameless face; otherwise the tiered badge. Unknown ids resolve to `null` —
 * callers fall back to the letter tile rather than a broken image.
 */
export function avatarBrandAssetUrl(
  id: AvatarBrandId,
  tier: AvatarBrandTier | null,
): string | null {
  const name = tier ? `${id}-${tier}.webp` : `${id}.webp`;
  const mod = ASSET_BY_NAME[name];
  return mod ? brandAssetSrc(mod) : null;
}

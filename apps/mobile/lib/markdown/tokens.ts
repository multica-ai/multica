/**
 * Design tokens for the in-house code block component. Prose markdown
 * styling lives in `markdown-style.ts` (passed to enriched-markdown).
 *
 * All values stick to Tailwind's built-in scale — no arbitrary `text-[Npx]`.
 */

/** Block code (fenced ``` blocks). */
export const CODE_BLOCK_TEXT_CLASS = "text-sm font-mono text-foreground";
// Vertical breathing room is handled by the parent `<View className="gap-3">`
// in `markdown.tsx`, not per-child margin — see the comment there.
export const CODE_BLOCK_CONTAINER_CLASS =
  "bg-code-surface border border-border rounded-lg p-3";
export const CODE_BLOCK_LANG_LABEL_CLASS =
  "text-xs uppercase tracking-wide text-muted-foreground mb-1";

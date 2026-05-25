/**
 * Deterministic color picker for inline-created labels. Ports the same
 * palette + hash from `packages/views/issues/components/pickers/label-picker.tsx`
 * so a name created on mobile gets the same color as the web equivalent
 * would have picked (behavioral parity per mobile CLAUDE.md "Data identity
 * must agree").
 */
const INLINE_COLORS = [
  "#ef4444",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#14b8a6",
  "#3b82f6",
  "#6366f1",
  "#a855f7",
  "#ec4899",
  "#64748b",
] as const;

export function pickInlineColor(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }
  return INLINE_COLORS[hash % INLINE_COLORS.length] ?? INLINE_COLORS[0];
}

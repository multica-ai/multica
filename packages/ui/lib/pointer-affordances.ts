/**
 * Pointer-gated hover affordances.
 *
 * iPadOS reports `(hover: hover)` for a finger — the first tap is a synthetic
 * hover, the second is the click — so `@media (hover: hover)` alone hides the
 * touch path and leaves the mouse path unreachable. Pair it with
 * `(pointer: fine)` (mouse / trackpad). Coarse pointers always see the control.
 *
 * These strings must stay complete Tailwind literals so `@source` can see them.
 * Desktop's CSS glob must include packages/ui TypeScript files, not only tsx.
 */

export const FINE_HOVER =
  "[@media(hover:hover)_and_(pointer:fine)]" as const;

/** Hide this control where a mouse can hover; keep it for touch. */
export const hideWhenFineHover =
  "[@media(hover:hover)_and_(pointer:fine)]:hidden" as const;

/**
 * Visible on touch. On a mouse, opacity 0 until the ancestor `.group` is
 * hovered or focus-within. Named groups (`group/row`) cannot use this helper
 * — use `hoverRevealRowOpacity` / `hoverRevealHeaderOpacity` / a complete
 * call-site literal instead of concatenating `FINE_HOVER`.
 */
export const hoverRevealOpacity =
  "opacity-100 [@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:group-focus-within:opacity-100" as const;

/** Same as `hoverRevealOpacity` for `group/row` list and table rows. */
export const hoverRevealRowOpacity =
  "opacity-100 [@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover/row:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:group-focus-within/row:opacity-100" as const;

/** Same as `hoverRevealOpacity` for `group/header` section / column headers. */
export const hoverRevealHeaderOpacity =
  "opacity-100 [@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover/header:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:group-focus-within/header:opacity-100" as const;

/** Same as `hoverRevealOpacity` for `group/code` copy / preview toolbars. */
export const hoverRevealCodeOpacity =
  "opacity-100 [@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover/code:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:group-focus-within/code:opacity-100" as const;

/**
 * Issue-list checkbox: always shown on touch so multi-select can start.
 * Mouse keeps the priority-icon ↔ checkbox hover swap.
 */
export const coarseAlwaysFineRowHoverBlock =
  "block [@media(hover:hover)_and_(pointer:fine)]:hidden [@media(hover:hover)_and_(pointer:fine)]:group-hover/row:block [@media(hover:hover)_and_(pointer:fine)]:group-focus-within/row:block" as const;

/** Priority icon paired with `coarseAlwaysFineRowHoverBlock`. */
export const fineOnlyUntilRowHoverHidden =
  "hidden [@media(hover:hover)_and_(pointer:fine)]:block [@media(hover:hover)_and_(pointer:fine)]:group-hover/row:hidden [@media(hover:hover)_and_(pointer:fine)]:group-focus-within/row:hidden" as const;

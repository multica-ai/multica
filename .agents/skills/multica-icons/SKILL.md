---
name: multica-icons
description: Use when a Multica web/desktop UI change needs an icon that `@multica/ui/icons` does not export yet, or when redrawing or reviewing a Multica glyph. Covers the grid, corner and stroke construction, the body/detail markers, and the generate-and-preview loop. Never reach for an icon library, inline SVG, or emoji instead.
---

# Drawing Multica icons

Every icon in the product is a Multica drawing exported from `@multica/ui/icons`. No icon library is installed, and lint rejects imports from one. When you need a new symbol, draw it.

## Before drawing

1. Search `packages/ui/icons/index.ts` for the name and for synonyms. If an existing glyph already means the same thing, add an alias to `ALIASES` in `drawings.mjs` instead of drawing a near-duplicate.
2. Pick a name that describes the symbol, not the feature (`Webhook`, not `IntegrationsNav`). Follow the existing PascalCase names.

## Where things live

- `packages/ui/icons/drawings/construction.mjs` holds the shared geometry helpers.
- `packages/ui/icons/drawings/drawings.mjs` holds every drawing as inner SVG markup, plus `ALIASES`.
- `packages/ui/icons/glyphs.ts` and `index.ts` are generated. Never edit them by hand; `icons.test.ts` fails when they drift.

## Construction

- **Grid.** 24×24 viewBox, with a 2px round-capped, round-joined stroke from the renderer. Keep geometry inside 2.5–21.5. Most silhouettes span 2.75–21.25. An object glyph whose longest side is under about 17 renders visibly smaller than its neighbours; only chevrons, `X` and similar marks stay small. Keep parallel strokes at least 3 apart so a gap survives at 14px. A stroke that crosses a shape, such as a pencil's cap line, ends exactly on the outline's centreline; the round cap pokes out if it runs past. When you scale or move a body, recompute these endpoints.
- **Corners.** Use `shape([[x, y, k], ...])` and `rect(x, y, w, h, k)`, which build continuous-curvature (squircle) corners. Typical `k` values:
  - 5–7 for large containers such as cards, windows and chat bubbles.
  - 2.5–4 for small parts.
  - 1–1.5 for tips and points.
  - 0 for a hard corner, which is rare.

  Do not hand-write `a r r` arc corners for rectangles.
- **Anything that points.** Chevrons, arrowheads, `< >` brackets, prompts and direction triangles (play, skip) turn at a sharp vertex: a straight polyline such as `M9 6L15 12 9 18`, or `head(x, y, dir, depth, half)` for an arrowhead with its tip at (x, y). Never round the tip with a curve or a `k` corner; the round stroke join is the only softening. The bend in a corner or U-turn arrow's shaft may stay rounded.
- **Checks.** Use `tick(a, v, b)`: two straight strokes meeting at a tight vertex. Never bow a stroke through a curve where it should turn a corner; a check or any other angular mark drawn that way reads as handwriting.
- **Dots.** `dot(x, y, r)` with r 1.1–1.5. Use dots only where they carry meaning: info, alert, eyes, status lights.
- **Polygons and stars.** `polygon(cx, cy, radii, count, k)`.
- **Reuse parts.** `file()`, `folderBody`, `bubble`, `ring`, `user()`, `link`, `cw` and `ccw` already exist. A variant (`…Off`, `…Plus`, `…X`) is the base drawing plus one stroke. A slash runs `m3 3 18 18`.

## Body and details

- Mark the main closed silhouette with `class="b"` (body). The active sidebar item fills it at 20% opacity.
  - Any icon that can appear in navigation must have a body. A test enforces this for route icons.
  - Only mark closed shapes. An open arc with a fill closes into a stray chord.
- Mark strokes that sit inside the body with `class="k"` (detail).
- **Detail budget.** At most two inner details. Every detail must carry meaning: a check, a symbol, a divider that defines the object. Drop decorative highlights, extra lines and texture. Icons render at 14px in dense rows, where a third detail turns into noise.

## Loop

1. Add the drawing to `DRAWINGS` in `drawings.mjs`. Keep sections grouped by subject.
2. Run `pnpm --filter @multica/ui icons --preview NewName Neighbour1 Neighbour2`. Open the printed HTML file in a browser and check four things:
   - At 48px: does the geometry sit on the grid, with even optical weight next to its neighbours?
   - At 18px and 14px: is it still legible, or do inner details merge?
   - In the tinted (active) tile: does only the silhouette fill?
   - Against similar glyphs: can it be told apart at a glance?
3. Run `pnpm --filter @multica/ui icons` to regenerate `glyphs.ts` and `index.ts`.
4. Import the new name from `@multica/ui/icons`. Then run `pnpm --filter @multica/ui test` and the typecheck for the package that uses it.

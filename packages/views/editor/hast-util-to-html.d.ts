/**
 * Type bridge for the untyped hast-util-to-html dependency used by readonly
 * editor rendering.
 *
 * Responsibilities:
 *   - Provide the single exported toHtml function type consumed by editor views.
 *
 * Boundaries:
 *   - Does not model the full upstream package surface.
 *   - Keeps dependency typing local until the package ships bundled types.
 */
declare module "hast-util-to-html" {
  export function toHtml(tree: unknown): string;
}

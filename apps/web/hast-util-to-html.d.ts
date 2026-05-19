/**
 * Type bridge for the untyped hast-util-to-html dependency reached through
 * @multica/views source during web app type checking.
 *
 * Responsibilities:
 *   - Provide the single exported toHtml function type consumed by editor views.
 *
 * Boundaries:
 *   - Does not model the full upstream package surface.
 *   - Exists only because the app compiler does not include package-local
 *     ambient declarations from workspace dependencies.
 */
declare module "hast-util-to-html" {
  export function toHtml(tree: unknown): string;
}

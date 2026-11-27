/**
 * Prefix a root-relative public asset path with the deployment's base path.
 *
 * Next.js prefixes the router, `next/link` and `/_next/` assets with
 * `basePath`, but not URLs written into metadata (icons), the web manifest or
 * hand-built `Location` headers. Behind a path mount (`/multica`) those still
 * point at the origin root and miss the app, so the tab shows no icon.
 * `NEXT_PUBLIC_BASE_PATH` is inlined at build time; unset means a root mount
 * and the path is returned unchanged.
 */
export function withBasePath(path: string, basePath = process.env.NEXT_PUBLIC_BASE_PATH ?? ""): string {
  const prefix = basePath.replace(/\/+$/, "");
  if (!prefix || !path.startsWith("/")) return path;
  if (path === prefix || path.startsWith(`${prefix}/`)) return path;
  return `${prefix}${path}`;
}

/**
 * NEXT_PUBLIC_BASE_PATH is inlined as a string literal by Next.js at build
 * time (via next.config.ts → NextConfig.basePath).  Changing BASE_PATH in the
 * environment after the bundle is built has no effect — a rebuild is required.
 *
 * Do NOT use this value at runtime to dynamically construct URLs; prefer the
 * Next.js <Link> component or the `useRouter` hook, which are already
 * base-path-aware.
 */
const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

/**
 * Prepend the application base path to an absolute-path string.
 * Only needed for raw string URLs that bypass Next.js routing (e.g. og-image
 * meta tags, sitemap entries).  Returns non-root-relative paths unchanged.
 */
export function publicUrl(path: string): string {
  if (!path.startsWith("/")) return path
  return basePath + path
}

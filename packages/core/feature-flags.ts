/**
 * Feature flags controlling the visibility of product surfaces.
 *
 * Each flag gates a feature end-to-end: when disabled, all UI entry points,
 * routes, and related components are hidden. When re-enabled, every surface
 * comes back without code changes — just set the env var.
 *
 * Flags are controlled via environment variables so operators can toggle them
 * per deployment without rebuilding:
 *
 *   Web (Next.js):    NEXT_PUBLIC_SKILLS_ENABLED=true
 *
 * All flags default to **disabled** (false) when the env var is unset or empty.
 */

function readEnv(name: string): string | undefined {
  try {
    return (process.env as Record<string, string | undefined>)?.[name];
  } catch {
    return undefined;
  }
}

function readBool(name: string): boolean {
  const raw = readEnv(name);
  if (!raw) return false;
  return raw === "true" || raw === "1";
}

/** When false, hides Skills from sidebar nav, agent create/edit, and agent detail tabs. */
export const SKILLS_ENABLED: boolean = readBool("NEXT_PUBLIC_SKILLS_ENABLED");

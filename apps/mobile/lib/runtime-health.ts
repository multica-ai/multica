/**
 * Mobile-local mirror of packages/views/runtimes/components/shared.tsx's
 * HEALTH_VISUAL dot-color mapping — mirrored, not imported, since that
 * file lives in packages/views (out of mobile's @multica/core-only
 * whitelist). Labels come from runtimes.json via i18n
 * (t(`health.${health}.label`)) instead of a hardcoded English fallback.
 */
import type { RuntimeHealth } from "@multica/core/runtimes";

export const HEALTH_DOT_CLASS: Record<RuntimeHealth, string> = {
  online: "bg-success",
  recently_lost: "bg-warning",
  offline: "bg-muted-foreground/40",
  about_to_gc: "bg-destructive",
};

import type { Workspace } from "../types";

export interface GravatarSettings {
  /**
   * When true, members without a custom avatar_url fall back to Gravatar
   * (a hash of their email is sent to gravatar.com). When false, the
   * initials fallback is used instead. Defaults to false — opt-in — because
   * Gravatar leaks email hashes to a third party on every render.
   */
  enabled: boolean;
}

export function deriveGravatarSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): GravatarSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  return {
    enabled: s.gravatar_enabled === true,
  };
}

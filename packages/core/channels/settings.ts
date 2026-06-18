import type { Workspace } from "../types";

export interface ChannelsSettings {
  enabled: boolean;
}

export function deriveChannelsSettings(
  workspace: Pick<Workspace, "settings"> | null | undefined,
): ChannelsSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  return { enabled: s.channels_enabled === true };
}

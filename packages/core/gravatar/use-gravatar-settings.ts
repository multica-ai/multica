import { useMemo } from "react";
// The hook module directly, not the `paths` barrel: `useWorkspaceId` reaches
// `useCurrentWorkspace` the same way, and the barrel re-exports members that
// view tests routinely mock down to the one or two they render.
import { useCurrentWorkspace } from "../paths/hooks";
import { deriveGravatarSettings, type GravatarSettings } from "./settings";

export function useGravatarSettings(): GravatarSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGravatarSettings(workspace), [workspace]);
}

import { useMemo } from "react";
import { useCurrentWorkspace } from "../paths";
import { deriveGravatarSettings, type GravatarSettings } from "./settings";

export function useGravatarSettings(): GravatarSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGravatarSettings(workspace), [workspace]);
}

import { useMemo } from "react";
import { resolveEnvironmentHint, type EnvironmentHint } from "../../../shared/environment-hint";

/** Active non-Cloud backend hint for Desktop chrome (sidebar, window title). */
export function useEnvironmentHint(): EnvironmentHint | null {
  return useMemo(
    () => resolveEnvironmentHint(window.desktopAPI.runtimeConfig),
    [],
  );
}

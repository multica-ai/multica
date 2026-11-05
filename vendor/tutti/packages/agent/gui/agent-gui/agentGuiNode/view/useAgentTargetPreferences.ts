import { useCallback, useEffect, useState } from "react";
import {
  AGENT_GUI_PROVIDER_RAIL_PREFERENCES_EVENT,
  agentGUIProviderRailOrderStorageKey,
  parseAgentGUIProviderRailPreferences
} from "../model/agentGuiProviderRailOrder";

/** Retain the installed home-target preferences after removing their hidden editor. */
export function useAgentTargetPreferences() {
  const storageKey = agentGUIProviderRailOrderStorageKey();
  const read = useCallback(
    () =>
      parseAgentGUIProviderRailPreferences(
        globalThis.localStorage?.getItem(storageKey)
      ),
    [storageKey]
  );
  const [preferences, setPreferences] = useState(read);
  useEffect(() => {
    const refresh = () => setPreferences(read());
    const onStorage = (event: StorageEvent) => {
      if (event.key === storageKey) refresh();
    };
    globalThis.addEventListener?.("storage", onStorage);
    globalThis.addEventListener?.(
      AGENT_GUI_PROVIDER_RAIL_PREFERENCES_EVENT,
      refresh
    );
    return () => {
      globalThis.removeEventListener?.("storage", onStorage);
      globalThis.removeEventListener?.(
        AGENT_GUI_PROVIDER_RAIL_PREFERENCES_EVENT,
        refresh
      );
    };
  }, [read, storageKey]);
  return { preferences };
}

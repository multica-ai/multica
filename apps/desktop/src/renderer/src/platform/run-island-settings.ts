import { useMemo, useSyncExternalStore } from "react";
import { defaultStorage } from "@multica/core/platform";
import {
  islandPreferences,
  type IslandPreferences,
} from "../../../shared/run-island";

const enabledKey = "multica:desktop:run-island";
const key = enabledKey + ":preferences";
const changed = "multica:run-island-changed";

function subscribe(listener: () => void) {
  const onStorage = (event: StorageEvent) => {
    if (!event.key || event.key === key || event.key === enabledKey) listener();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(changed, listener);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(changed, listener);
  };
}

export function useRunIslandEnabled() {
  return useSyncExternalStore(
    subscribe,
    () => defaultStorage.getItem(enabledKey) === "true",
  );
}

export function setRunIslandEnabled(enabled: boolean) {
  defaultStorage.setItem(enabledKey, String(enabled));
  window.dispatchEvent(new Event(changed));
}

function parse(raw: string | null) {
  try {
    return islandPreferences(raw && JSON.parse(raw));
  } catch {
    return islandPreferences(null);
  }
}

export function useIslandPreferences() {
  const raw = useSyncExternalStore(subscribe, () =>
    defaultStorage.getItem(key),
  );
  return useMemo(() => parse(raw), [raw]);
}

export function changeIslandPreferences(
  update: (current: IslandPreferences) => IslandPreferences,
) {
  defaultStorage.setItem(
    key,
    JSON.stringify(update(parse(defaultStorage.getItem(key)))),
  );
  window.dispatchEvent(new Event(changed));
}

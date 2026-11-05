import { comparer, computed, reaction } from "mobx";
import { useCallback, useMemo, useSyncExternalStore } from "react";

/** Subscribe to Model values from hooks whose callers are not observers yet. */
export function useModelValue<T>(
  read: () => T,
  deps: readonly unknown[],
  equals?: (left: T, right: T) => boolean
): T {
  const value = useMemo(
    () =>
      computed(read, { equals: equals ?? comparer.default, keepAlive: true }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    deps
  );
  const subscribe = useCallback(
    (listener: () => void) =>
      reaction(
        () => value.get(),
        () => listener()
      ),
    [value]
  );
  const snapshot = useCallback(() => value.get(), [value]);
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}

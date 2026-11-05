import { useMemo, useRef } from "react";

/** Stable identity with the current render's implementation, including render slots. */
export function usePersistCallback<T extends (...args: never[]) => unknown>(
  callback: T
): T {
  const current = useRef(callback);
  current.current = useMemo(() => callback, [callback]);
  const stable = useRef<T | null>(null);
  if (!stable.current) {
    stable.current = function (this: unknown, ...args: Parameters<T>) {
      return current.current.apply(this, args);
    } as T;
  }
  return stable.current;
}

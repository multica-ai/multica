"use client";

import { useEffect, useState } from "react";

/** The last settled value; disabling cancels pending updates and retains it. */
export function useDebouncedValue<T>(value: T, delayMs: number, enabled = true): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    if (!enabled) return;
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs, enabled]);
  return debounced;
}

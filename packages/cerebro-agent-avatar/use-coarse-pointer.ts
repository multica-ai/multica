"use client";

import { useEffect, useState } from "react";

/**
 * True when the primary pointer is coarse (touch). Used to switch hover-only
 * surfaces to tap-open. Matches the `(pointer: coarse)` signal already used
 * by submit-on-enter preferences.
 */
export function useIsCoarsePointer(): boolean {
  const [coarse, setCoarse] = useState<boolean>(() => readCoarsePointer());
  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const mql = window.matchMedia("(pointer: coarse)");
    const onChange = () => setCoarse(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);
  return coarse;
}

export function readCoarsePointer(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia("(pointer: coarse)").matches;
}

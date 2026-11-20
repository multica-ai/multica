"use client";

import { useEffect, useState } from "react";
import { detectOS, type DetectResult } from "../../utils/os-detect";

/**
 * The server's platform guess, refined once the browser can say more: a
 * confident CPU architecture on Chromium, or an iPad posing as a Mac.
 */
export function useDetectedPlatform(initial: DetectResult): DetectResult {
  const [detected, setDetected] = useState(initial);
  const initialOS = initial.os;

  useEffect(() => {
    let active = true;
    void detectOS().then((result) => {
      if (active && (result.archConfident || result.os !== initialOS)) setDetected(result);
    });
    return () => {
      active = false;
    };
  }, [initialOS]);

  return detected;
}

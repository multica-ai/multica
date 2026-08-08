"use client";

import { useEffect, useRef } from "react";
import {
  QUALIFIED_VIEW_DELAY_MS,
  captureQualifiedLandingView,
  claimQualifiedLandingView,
  isQualifiedLandingContext,
} from "../analytics";

export function LandingFunnelTracker() {
  const captured = useRef(false);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;

    const clearTimer = () => {
      if (timer !== null) clearTimeout(timer);
      timer = null;
    };

    const schedule = () => {
      clearTimer();
      if (captured.current) return;
      if (
        !isQualifiedLandingContext({
          visibilityState: document.visibilityState,
          webdriver: navigator.webdriver === true,
          userAgent: navigator.userAgent,
        })
      ) {
        return;
      }

      timer = setTimeout(() => {
        timer = null;
        if (captured.current || document.visibilityState !== "visible") return;
        captured.current = true;
        let storage: Storage | null = null;
        try {
          storage = window.sessionStorage;
        } catch {
          // Access itself can throw in hardened privacy modes.
        }
        if (claimQualifiedLandingView(storage)) {
          captureQualifiedLandingView();
        }
      }, QUALIFIED_VIEW_DELAY_MS);
    };

    schedule();
    document.addEventListener("visibilitychange", schedule);
    return () => {
      clearTimer();
      document.removeEventListener("visibilitychange", schedule);
    };
  }, []);

  return null;
}

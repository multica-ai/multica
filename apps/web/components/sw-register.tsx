"use client";

import { useEffect } from "react";

/**
 * Registers the Multica service worker (`/sw.js`) so the app meets PWA
 * installability criteria. Renders nothing.
 *
 * Guards:
 *  - Production only. In `next dev` a service worker would aggressively shadow
 *    HMR and confuse local development, so we skip registration entirely.
 *  - Secure context only. Browsers refuse to register a service worker on a
 *    non-secure origin (anything other than https:// or localhost). We emit a
 *    single console warning so self-hosters serving over plain http:// know why
 *    install is unavailable.
 */
export function ServiceWorkerRegister() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!("serviceWorker" in navigator)) return;
    if (process.env.NODE_ENV !== "production") return;

    const register = () => {
      if (!window.isSecureContext) {
        console.warn(
          "[Multica PWA] Skipping service worker registration: not a secure context. " +
            "Serve the app over HTTPS (or use localhost) to enable installation.",
        );
        return;
      }
      navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch((err) => {
        // Registration failures are non-fatal — the app works without the
        // worker. Surface them so self-hosters can debug install issues in prod.
        console.warn("[Multica PWA] Service worker registration failed:", err);
      });
    };

    // Defer to the `load` event so registration never competes with the initial
    // render for bandwidth. If the page is already loaded (e.g. client-side
    // navigation mounted this late), register immediately.
    if (document.readyState === "complete") {
      register();
      return;
    }
    window.addEventListener("load", register, { once: true });
    return () => window.removeEventListener("load", register);
  }, []);

  return null;
}

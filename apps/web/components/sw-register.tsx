"use client";

import { useEffect } from "react";

export function ServiceWorkerRegister() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!("serviceWorker" in navigator)) return;
    if (process.env.NODE_ENV !== "production") return;

    const onLoad = () => {
      if (!window.isSecureContext) {
        // Browsers refuse to register service workers on non-secure origins
        // (anything other than https:// or localhost). Surface a one-line hint
        // so self-hosters hitting the app over plain http:// know why install
        // is unavailable.
        console.warn(
          "[Multica PWA] Skipping service worker registration: not a secure context. " +
            "Serve the app over HTTPS to enable install/offline support."
        );
        return;
      }
      navigator.serviceWorker
        .register("/sw.js", { scope: "/" })
        .catch(() => {
          // Registration failures are non-fatal — the app works without the SW.
        });
    };

    if (document.readyState === "complete") onLoad();
    else window.addEventListener("load", onLoad, { once: true });

    return () => window.removeEventListener("load", onLoad);
  }, []);

  return null;
}

"use client";

import { useEffect } from "react";
import { syncStaticAssetServiceWorker } from "@/lib/register-static-sw";

export function StaticAssetSW() {
  useEffect(() => {
    if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) {
      return;
    }
    return syncStaticAssetServiceWorker({
      production: process.env.NODE_ENV === "production",
      serviceWorker: navigator.serviceWorker,
      documentReadyState: document.readyState,
      onLoad: (handler) => {
        window.addEventListener("load", handler);
        return () => window.removeEventListener("load", handler);
      },
    });
  }, []);
  return null;
}

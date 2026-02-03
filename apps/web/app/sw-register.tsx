"use client";

import { useEffect } from "react";

export function ServiceWorkerRegister() {
  useEffect(() => {
    if ("serviceWorker" in navigator) {
      navigator.serviceWorker
        .register("/sw.js", {
          scope: "/",
          updateViaCache: "none",
        })
        .catch((err) => {
          if (process.env.NODE_ENV === "development") {
            console.warn("SW registration failed:", err);
          }
        });
    }
  }, []);

  return null;
}

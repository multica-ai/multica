"use client";

import { useEffect } from "react";
import { captureException } from "@multica/core/analytics";

/**
 * Route-level error boundary for the web app. Next.js renders this (replacing
 * the root layout) when an error escapes everything below it — the full-page
 * white-screen case. React catches these before they reach window.onerror, so
 * posthog-js's automatic exception capture never sees them; we report them
 * explicitly here. Section-level failures are handled in place by
 * `@multica/ui` ErrorBoundary and don't reach this far.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    captureException(error, { source: "global-error", digest: error.digest });
  }, [error]);

  return (
    <html>
      <body
        style={{
          display: "flex",
          minHeight: "100vh",
          alignItems: "center",
          justifyContent: "center",
          fontFamily: "system-ui, sans-serif",
        }}
      >
        <div style={{ maxWidth: 420, textAlign: "center" }}>
          <h1 style={{ fontSize: 18, fontWeight: 600 }}>Something went wrong</h1>
          <p style={{ marginTop: 8, color: "#666" }}>
            The page hit an unexpected error. Try reloading.
          </p>
          <button
            type="button"
            onClick={reset}
            style={{
              marginTop: 16,
              padding: "8px 16px",
              borderRadius: 6,
              border: "1px solid #ccc",
              cursor: "pointer",
            }}
          >
            Reload
          </button>
        </div>
      </body>
    </html>
  );
}

"use client";

import { useEffect } from "react";
import { captureException } from "@multica/core/analytics";
import { Button } from "@multica/ui/components/ui/button";
import { AlertTriangle, RotateCw } from "@multica/ui/icons";
import { useT } from "@multica/views/i18n";

/**
 * Page-level error boundary for every workspace page.
 *
 * Next.js mounts this inside the (dashboard) layout, so when a page throws
 * only the content area is replaced: the sidebar, header and floating chat
 * stay usable. Without it, an error in any page that has no local
 * ErrorBoundary (chat, docs, agents, runtimes, settings, ...) escaped to
 * app/global-error.tsx and blanked the whole app. Next resets this boundary
 * on navigation, so moving to another page via the sidebar also recovers.
 */
export default function DashboardError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const { t } = useT("ui");

  useEffect(() => {
    captureException(error, { source: "dashboard-error", digest: error.digest });
  }, [error]);

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-title font-semibold">
          {t(($) => $.error_boundary.title)}
        </h2>
        <p className="max-w-lg truncate text-body text-muted-foreground">
          {error.message || t(($) => $.error_boundary.description)}
        </p>
      </div>
      <Button type="button" variant="outline" onClick={reset}>
        <RotateCw className="mr-2 h-4 w-4" aria-hidden="true" />
        {t(($) => $.error_boundary.try_again)}
      </Button>
    </div>
  );
}

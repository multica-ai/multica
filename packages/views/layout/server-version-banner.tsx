"use client";

import { TriangleAlert, X } from "lucide-react";
import { useServerVersionMismatch } from "@multica/core/version";
import { useT } from "../i18n";

/**
 * Read-only startup banner (#5848): shown when this app's version is provably
 * newer than the server's, meaning the server may lack endpoints the app
 * calls. Never blocks anything; dismissible for the session. Renders nothing
 * when the comparison is unknown (cloud, dev builds) or the server is newer
 * (the client update flow owns that case).
 */
export function ServerVersionBanner() {
  const { t } = useT("layout");
  const { show, appVersion, serverVersion, dismiss } = useServerVersionMismatch();

  if (!show) return null;

  return (
    <div
      role="status"
      className="flex items-center gap-2 border-b bg-muted/30 px-4 py-2 text-caption text-muted-foreground"
    >
      <TriangleAlert className="h-3.5 w-3.5 shrink-0" aria-hidden />
      <span className="flex-1">
        {t(($) => $.version_mismatch.message, { serverVersion, appVersion })}
      </span>
      <button
        type="button"
        aria-label={t(($) => $.version_mismatch.dismiss)}
        onClick={dismiss}
        className="shrink-0 rounded p-0.5 hover:bg-muted hover:text-foreground"
      >
        <X className="h-3.5 w-3.5" aria-hidden />
      </button>
    </div>
  );
}

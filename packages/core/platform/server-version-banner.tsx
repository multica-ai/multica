"use client";

import { useTranslation } from "react-i18next";
import { useConfigStore, type ServerVersionCompat } from "../config";

// Generic upgrade command for self-hosted operators. The exact compose file
// varies by deployment, so we keep it to the standard `docker compose` form
// the operator runs from their deployment directory.
const UPGRADE_COMMAND =
  "docker compose pull backend frontend && docker compose up -d backend frontend";

/**
 * Global, non-blocking banner shown when the self-hosted server version is
 * older than the desktop app. Surfaces the "updated the app but not the
 * server" mismatch that otherwise manifests as opaque "load more failed"
 * errors on version-gated features (e.g. the issue board). Renders nothing
 * when the server version is unknown (managed cloud omits it) or compatible.
 */
export function ServerVersionBanner() {
  const { t } = useTranslation("layout");
  const compat = useConfigStore((s) => s.serverVersionCompat);
  if (!compat || compat.state !== "too_old") return null;
  return (
    <div
      role="alert"
      className="fixed inset-x-0 top-0 z-50 border-b border-amber-500/40 bg-amber-500/10 px-4 py-2 text-sm text-amber-900 dark:text-amber-200"
    >
      <div className="mx-auto flex max-w-5xl items-center gap-3">
        <span className="flex-1">
          {t("help.server_version_too_old", { current: compat.current, min: compat.min })}
        </span>
        <code className="shrink-0 rounded bg-black/10 px-2 py-1 font-mono text-xs">
          {UPGRADE_COMMAND}
        </code>
      </div>
    </div>
  );
}

"use client";

import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useT } from "../i18n";

/**
 * Full-screen workspace loader. Renders IN PLACE OF the dashboard during:
 *  - initial dashboard mount (workspace resolving from URL slug + list cache)
 *  - workspace switch (refetching core workspace data with the new header)
 *
 * This is a GATE, not an overlay — sidebar/content do not render behind it.
 * The gate only opens once the current workspace id has been set on the
 * workspace-storage singleton AND all core queries for the target
 * workspace have been freshly fetched.
 */
export function WorkspaceLoader({ name }: { name?: string | null }) {
  const { t } = useT("layout");
  return (
    <div
      className="flex h-svh w-full items-center justify-center bg-background"
      aria-live="polite"
      role="status"
    >
      <div className="flex flex-col items-center gap-4">
        <MulticaIcon className="size-8 animate-pulse" />
        {name ? (
          <p className="text-sm text-muted-foreground">
            {t(($) => $.workspace_loader.loading_named_prefix)}{" "}
            <span className="font-medium text-foreground">{name}</span>…
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.workspace_loader.loading_workspace)}</p>
        )}
      </div>
    </div>
  );
}

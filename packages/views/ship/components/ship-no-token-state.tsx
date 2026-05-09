"use client";

import { KeyRound } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Rendered when Ship Hub is enabled but `github_token_set` is false. The
 * project-listing endpoint returns 200 with an empty list in that case
 * (no token → backend can't sync), so we detect "feature on but no token"
 * and steer the admin to Settings.
 */
export function ShipNoTokenState() {
  const { t } = useT("ship");
  const wsPaths = useWorkspacePaths();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
      <KeyRound className="size-10 text-muted-foreground/50" />
      <h2 className="text-lg font-semibold text-foreground">
        {t(($) => $.empty.no_token_title)}
      </h2>
      <p className="max-w-md text-sm text-muted-foreground">
        {t(($) => $.empty.no_token_description)}
      </p>
      <Button variant="outline" size="sm" render={<AppLink href={wsPaths.settings()} />}>
        {t(($) => $.empty.configure_token)}
      </Button>
    </div>
  );
}

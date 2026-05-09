"use client";

import { GitPullRequest } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Rendered when Ship Hub is enabled and a token is configured but no
 * project in the workspace has a github_repo resource attached. Points
 * the user at the projects list so they can attach a repo.
 */
export function ShipEmptyState() {
  const { t } = useT("ship");
  const wsPaths = useWorkspacePaths();
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
      <GitPullRequest className="size-10 text-muted-foreground/50" />
      <h2 className="text-lg font-semibold text-foreground">
        {t(($) => $.empty.no_projects_title)}
      </h2>
      <p className="max-w-md text-sm text-muted-foreground">
        {t(($) => $.empty.no_projects_description)}
      </p>
      <Button variant="outline" size="sm" render={<AppLink href={wsPaths.projects()} />}>
        {t(($) => $.empty.go_to_projects)}
      </Button>
    </div>
  );
}

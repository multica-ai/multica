"use client";

import { buttonVariants } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../i18n";
import { AppLink } from "../navigation";

/** Stands in for an add action the workspace's plan does not include. */
export function UpgradeToAddLink() {
  const { t } = useT("billing");
  const paths = useWorkspacePaths();
  return (
    <AppLink href={`${paths.settings()}?tab=billing`} className={buttonVariants({ size: "xs", variant: "outline" })}>
      {t(($) => $.paid_feature.upgrade_to_add)}
    </AppLink>
  );
}

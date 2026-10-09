"use client";

import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";

/**
 * `v2` beside a file that was uploaded more than once. Decorative: the
 * accessible name of whatever it sits in carries the version in words.
 */
export function VersionBadge({
  version,
  className,
}: {
  version: number;
  className?: string;
}) {
  const { t } = useT("issues");
  return (
    <span
      className={cn(
        "shrink-0 rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground",
        className,
      )}
      aria-hidden
    >
      {t(($) => $.deliverables.version_short, { version })}
    </span>
  );
}

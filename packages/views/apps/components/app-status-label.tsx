"use client";

import { AlertTriangle, Clock } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { AppAccountStatus } from "../../common/app-status";
import { useT } from "../../i18n";

/** An account status in the shared vocabulary, with `detail` after it.
 * `label` rewords it for one context while keeping the status's look. */
export function AppStatusLabel({
  status,
  label: override,
  detail,
  className,
}: {
  status: AppAccountStatus;
  label?: string;
  detail?: string;
  className?: string;
}) {
  const { t } = useT("settings");
  const label = override ?? t(($) => $.connected_apps.status[status]);
  const text = <span className="truncate">{detail ? `${label} · ${detail}` : label}</span>;
  switch (status) {
    case "connected":
      return (
        <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
          <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-success" />
          {text}
        </span>
      );
    case "needs_reconnecting":
      return (
        <span className={cn("inline-flex min-w-0 items-center gap-1 text-warning", className)}>
          <AlertTriangle aria-hidden="true" className="size-3 shrink-0" />
          {text}
        </span>
      );
    case "requested":
      return (
        <span className={cn("inline-flex min-w-0 items-center gap-1 text-muted-foreground", className)}>
          <Clock aria-hidden="true" className="size-3 shrink-0" />
          {text}
        </span>
      );
    case "not_connected":
      return (
        <span className={cn("inline-flex min-w-0 items-center gap-1.5 text-muted-foreground", className)}>
          <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full border border-muted-foreground" />
          {text}
        </span>
      );
  }
}

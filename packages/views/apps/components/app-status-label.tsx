"use client";

import { AlertTriangle } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { needsAttention, type AppAccountStatus } from "../../common/app-status";
import { useT } from "../../i18n";

/** An account status in the shared vocabulary, with `detail` after it. */
export function AppStatusLabel({
  status,
  detail,
  className,
}: {
  status: AppAccountStatus;
  detail?: string;
  className?: string;
}) {
  const { t } = useT("settings");
  const label = t(($) => $.connected_apps.status[status]);
  if (!needsAttention(status)) {
    return (
      <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
        <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-success" />
        <span className="truncate">{detail ? `${label} · ${detail}` : label}</span>
      </span>
    );
  }
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1 text-warning", className)}>
      <AlertTriangle aria-hidden="true" className="size-3 shrink-0" />
      <span className="truncate">{detail ? `${label} · ${detail}` : label}</span>
    </span>
  );
}

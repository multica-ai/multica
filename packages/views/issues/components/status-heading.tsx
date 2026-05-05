"use client";

import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "./status-icon";
import { useT } from "@multica/i18n/react";

export function StatusHeading({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const t = useT("status");
  return (
    <div className="flex items-center gap-2">
      <span className="inline-flex items-center gap-1.5 text-xs font-semibold">
        <StatusIcon status={status} className="h-3 w-3" />
        {t(status)}
      </span>
      <span className="text-xs text-muted-foreground">{count}</span>
    </div>
  );
}

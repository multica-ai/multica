"use client";

import type { RuntimeDevice } from "@multica/core/types";
import { ProviderLogo } from "./provider-logo";

export function TaskRuntimeBadge({
  runtime,
  size = "sm",
}: {
  runtime: RuntimeDevice | null;
  size?: "sm" | "xs";
}) {
  if (!runtime) return null;
  const iconCls = size === "xs" ? "h-3 w-3" : "h-3.5 w-3.5";
  const textCls = size === "xs" ? "text-[10px]" : "text-xs";
  const dotCls = "h-1.5 w-1.5";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 font-medium text-muted-foreground ${textCls}`}
      title={`Runtime: ${runtime.name} (${runtime.runtime_mode})`}
    >
      <ProviderLogo provider={runtime.provider} className={`${iconCls} shrink-0`} />
      <span className="truncate max-w-[12ch]">{runtime.name}</span>
      <span
        className={`${dotCls} shrink-0 rounded-full ${
          runtime.status === "online" ? "bg-success" : "bg-muted-foreground/40"
        }`}
      />
    </span>
  );
}

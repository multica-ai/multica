"use client";

import { Info } from "lucide-react";

export function BuiltinReadOnlyBanner() {
  return (
    <div
      role="status"
      className="flex items-center gap-2 rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs"
    >
      <Info className="h-3.5 w-3.5 shrink-0 text-amber-500" />
      <span className="text-amber-700 dark:text-amber-400">
        内置 Agent — 仅管理员可编辑
      </span>
    </div>
  );
}

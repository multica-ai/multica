"use client";

import type { WorkflowNode } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";

export interface CompactNodeCardProps {
  node: WorkflowNode;
  workerName: string | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string, focus: "worker") => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

export function CompactNodeCard({
  node,
  workerName,
  plugin,
  onClick,
  isSelected = false,
  elementRef,
}: CompactNodeCardProps) {
  const { t } = useT("workflows");
  const displayName = plugin?.name ?? node.title;

  const subtitleLabel = (() => {
    if (workerName) return workerName;
    const wt = node.worker_type;
    const typeLabel =
      wt === "human" ? t(($) => $.node.worker_type_human)
      : wt === "squad" ? t(($) => $.node.worker_type_squad)
      : t(($) => $.node.worker_type_agent);
    return `${typeLabel} · ${t(($) => $.overview.detail_panel.not_configured)}`;
  })();

  return (
    <button
      type="button"
      data-testid={`compact-node-card-${node.id}`}
      onClick={() => onClick(node.id, "worker")}
      ref={elementRef}
      className={cn(
        "group flex h-16 w-56 shrink-0 flex-col gap-1.5 rounded-lg border border-slate-300/90 bg-white p-2.5 text-left shadow-[0_1px_2px_rgba(15,23,42,0.08)] transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-primary/45 hover:bg-background hover:shadow-[0_8px_20px_rgba(15,23,42,0.08)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-primary/55 bg-background shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]",
      )}
      aria-pressed={isSelected}
    >
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>

      <div className="mt-auto flex items-center gap-1.5">
        <span
          className={cn(
            "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
            workerName ? "bg-[var(--success)]" : "bg-muted-foreground/40",
          )}
        />
        <span className="truncate text-[11px] text-muted-foreground">
          {subtitleLabel}
        </span>
      </div>
    </button>
  );
}

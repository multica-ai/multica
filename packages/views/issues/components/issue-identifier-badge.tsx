import type { Issue } from "@multica/core/types";
import { StatusIcon } from "./status-icon";
import { cn } from "@multica/ui/lib/utils";

interface IssueIdentifierBadgeProps {
  issue: Issue;
  onCopy: () => void;
  className?: string;
}

export function IssueIdentifierBadge({ issue, onCopy, className }: IssueIdentifierBadgeProps) {
  return (
    <button
      type="button"
      onClick={onCopy}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border bg-muted/60 px-2 py-0.5 text-xs font-medium tabular-nums text-foreground",
        "hover:bg-muted hover:border-border/80 transition-colors cursor-pointer",
        className,
      )}
      title={issue.identifier}
    >
      <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
      {issue.identifier}
    </button>
  );
}

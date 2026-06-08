import type { Issue } from "@multica/core/types";
import { isIssueApprovalRequired } from "@multica/core/issues/queries";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export function ApprovalRequiredBadge({
  issue,
  className,
}: {
  issue: Issue;
  className?: string;
}) {
  const { t } = useT("issues");
  if (!isIssueApprovalRequired(issue)) return null;

  const label = t(($) => $.card.approval_required) || "Approval";

  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] font-medium leading-none text-destructive",
        className,
      )}
      title={label}
    >
      <span className="size-1.5 rounded-full bg-destructive" />
      <span>{label}</span>
    </span>
  );
}

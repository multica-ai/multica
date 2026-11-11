import { issueBehavesAs } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { CircleAlert } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { ChildProgress } from "./list-row";

export function OpenSubIssuesIndicator({
  issue,
  childProgress,
  className,
}: {
  issue: Pick<Issue, "status" | "status_category">;
  childProgress?: ChildProgress;
  className?: string;
}) {
  const { t } = useT("issues");
  if (!childProgress || (issue.status !== "in_review" && !issueBehavesAs(issue, "done"))) {
    return null;
  }

  // The child-progress endpoint includes both done and cancelled children in done.
  const count = childProgress.total - childProgress.done;
  if (count <= 0) return null;

  return (
    <span
      className={cn(
        "inline-flex max-w-full shrink-0 items-center gap-1 rounded-full bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning",
        className,
      )}
    >
      <CircleAlert aria-hidden="true" className="size-3 shrink-0" />
      <span className="truncate">{t(($) => $.card.open_sub_issues, { count })}</span>
    </span>
  );
}

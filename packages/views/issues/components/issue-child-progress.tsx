import { issueBehavesAs } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import type { ChildProgress } from "./list-row";
import { ProgressRing } from "./progress-ring";

export function IssueChildProgress({
  issue,
  progress,
}: {
  issue: Pick<Issue, "status" | "status_category">;
  progress: ChildProgress;
}) {
  const { t } = useT("issues");
  // The permission-filtered count includes done and cancelled children as done.
  const remaining = progress.total - progress.done;
  if (issueBehavesAs(issue, "done") && remaining > 0) {
    return (
      <span className="text-micro font-medium text-warning">
        {t(($) => $.card.unfinished_sub_issues, { count: remaining })}
      </span>
    );
  }

  return (
    <>
      <ProgressRing done={progress.done} total={progress.total} size={14} />
      <span className="text-micro text-muted-foreground tabular-nums font-medium">
        {progress.done}/{progress.total}
      </span>
    </>
  );
}

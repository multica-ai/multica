import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { IssueTimePicker } from "@multica/cerebro-issue-datetime/views";

interface Props {
  issueId: string;
  kind: "start" | "due";
  date: string | null;
}

/** Keeps Cerebro's optional time-of-day control aligned with Table date cells. */
export function IssueTableTimePicker({ issueId, kind, date }: Props) {
  const workspaceId = useWorkspaceId();
  const enabled = useFeatureFlag("cerebro_issue_date_times");

  if (!enabled || !date) return null;

  return (
    <IssueTimePicker
      workspaceId={workspaceId}
      issueId={issueId}
      kind={kind}
      className="mt-1"
    />
  );
}

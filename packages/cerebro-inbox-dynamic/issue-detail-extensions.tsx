// FIR-4918 — the sidebar extensions slot for an issue opened from the inbox.
//
// IssueDetail renders whatever is passed as `extensions` in its sidebar, above
// Pull requests. The issue routes (apps/web .../cerebro-issue-detail-route.tsx
// and apps/desktop .../issue-detail-page.tsx) mount Rounds AND References
// there; the inbox mounted Rounds only, so an issue opened from the inbox was
// missing References entirely — not hidden behind the collapsed panel, absent.
// Keeping the slot in one component is what makes the two surfaces stay equal.
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { IssueRoundsSection } from "@multica/cerebro-rounds";
import { IssueReferenceList } from "@multica/cerebro-references/views";

export function InboxIssueDetailExtensions({ issueId }: { issueId: string }) {
  const roundsEnabled = useFeatureFlag("cerebro_inbox_rounds");
  const referencesEnabled = useFeatureFlag("cerebro_references");
  return (
    <>
      {roundsEnabled && <IssueRoundsSection issueId={issueId} />}
      {referencesEnabled && <IssueReferenceList issueId={issueId} />}
    </>
  );
}

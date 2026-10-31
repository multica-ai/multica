"use client";

import { GitPullRequest } from "@multica/ui/icons";
import { useT } from "../i18n";
import { IssueSurface } from "../issues/surface/issue-surface";
import { IssueViewHeader } from "./view-header";

const AWAITING_MERGE_SCOPE = { type: "awaiting_merge" } as const;
const AWAITING_MERGE_BASE = { kind: "awaiting_merge" } as const;

/** Built-in view: open issues with an open PR, grouped by whether it can merge. */
export function AwaitingMergePage() {
  const { t } = useT("issues");

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueSurface
        scope={AWAITING_MERGE_SCOPE}
        modes={["list"]}
        batchToolbar="never"
        renderHeader={({ controller }) => <IssueViewHeader controller={controller} base={AWAITING_MERGE_BASE} />}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <GitPullRequest className="h-10 w-10 text-faint-foreground" />
            <p className="text-body">{t(($) => $.views.awaiting_merge.empty_title)}</p>
            <p className="text-caption">{t(($) => $.views.awaiting_merge.empty_description)}</p>
          </div>
        )}
      />
    </div>
  );
}

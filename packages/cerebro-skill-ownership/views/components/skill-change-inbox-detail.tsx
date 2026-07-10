"use client";

// FIR-2742 — inbox detail for a skill change-request notification. Previously
// clicking the row pushed a full navigation to the skill page, taking over the
// window. Now the row opens here in the inbox pane: it lists what changed (the
// diff) and offers an explicit "Open in new window" so the reviewer stays in
// their inbox flow and only pops out to the full skill page when they choose to.

import { Clock, ExternalLink, GitPullRequestArrow, MessageSquare } from "lucide-react";
import type { InboxItem } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { SkillDiffView } from "./skill-diff-view";

interface Props {
  item: InboxItem;
  /** Localised "N ago" for the notification time. */
  timeAgo?: string;
}

export function SkillChangeInboxDetail({ item, timeAgo }: Props) {
  const paths = useWorkspacePaths();
  const d = item.details ?? {};
  const skillId = d.skill_id;
  const crId = d.change_request_id;
  const skillName = d.skill_name;
  const title = d.title || item.title;
  const baseVersion = d.base_version;
  const proposedVersion = d.proposed_version;
  const reviewed = item.type === "skill_change_request_reviewed";

  const openInNewWindow = () => {
    if (!skillId) return;
    const url = `${paths.skillDetail(skillId)}${crId ? `?cr=${crId}` : ""}`;
    window.open(url, "_blank", "noopener,noreferrer");
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-3 sm:p-6">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-lg font-semibold">
            <GitPullRequestArrow className="h-4 w-4 shrink-0 text-warning" />
            <span className="min-w-0 truncate">{title}</span>
          </h2>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            {skillName && (
              <span className="font-medium text-foreground">{skillName}</span>
            )}
            {baseVersion && proposedVersion && (
              <span>
                · {baseVersion} → {proposedVersion}
              </span>
            )}
            {timeAgo && (
              <>
                <Clock className="h-3 w-3" />
                {timeAgo}
              </>
            )}
            {reviewed && <span>· reviewed</span>}
          </div>
        </div>
        {skillId && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="shrink-0 gap-1.5"
            onClick={openInNewWindow}
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Open in new window
          </Button>
        )}
      </div>

      {item.body && (
        <div className="rounded bg-muted/40 px-2.5 py-1.5 text-xs text-muted-foreground">
          <MessageSquare className="mr-1 inline h-3 w-3" />
          {item.body}
        </div>
      )}

      {d.base_content !== undefined && d.proposed_content !== undefined ? (
        <SkillDiffView
          base={d.base_content}
          proposed={d.proposed_content}
          baseLabel={baseVersion ? `current (${baseVersion})` : "current"}
          proposedLabel={proposedVersion ?? "proposed"}
        />
      ) : d.diff ? (
        <pre className="overflow-x-auto rounded-md border bg-muted/30 p-3 font-mono text-xs leading-relaxed">
          {d.diff}
        </pre>
      ) : null}
    </div>
  );
}

"use client";

import { useQuery } from "@tanstack/react-query";
import { Badge } from "@multica/ui/components/ui/badge";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { ActorAvatar } from "@multica/views/common/actor-avatar";

/**
 * Render an ISO timestamp as a short, locale-aware "MMM D, YYYY, HH:MM" string.
 * Empty / unparseable input renders nothing so a missing date never shows
 * "Invalid Date".
 */
export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * The shared "people + things + updated" header that sits under the title of
 * any artifact-like surface — documents AND notes (FIR-1647, request 8). It was
 * extracted from the Documents view's inline ConnectionRow so both surfaces show
 * the same metadata strip: who owns it, who it was made for, what it is linked
 * to, and when it was last updated.
 *
 * It is intentionally presentational over plain ids rather than over an Artifact
 * or a Note, so each surface can map its own wire shape onto it:
 *  - Documents pass author/requester/issue/project/origin straight from the
 *    artifact record.
 *  - Notes pass author_type="member" + owner_id and their updated_at; notes have
 *    no inline issue/project columns (they link through references), so those
 *    props are simply omitted.
 */
export interface EntityMetaHeaderProps {
  authorType: string;
  authorId: string;
  updatedAt: string;
  /** A member this was created on behalf of, shown only when distinct from the author. */
  requesterUserId?: string | null;
  /** The issue this is filed on (deep-links to the issue). */
  issueId?: string | null;
  /** The project this is filed on (deep-links to the project). */
  projectId?: string | null;
  /** The issue this originated from, shown only when distinct from issueId. */
  originIssueId?: string | null;
  className?: string;
}

export function EntityMetaHeader({
  authorType,
  authorId,
  updatedAt,
  requesterUserId,
  issueId,
  projectId,
  originIssueId,
  className,
}: EntityMetaHeaderProps) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { getActorName, getMemberName } = useActorName();

  // Resolve the issue title / identifier when there is any issue link.
  const linkedIssueId = issueId ?? originIssueId;
  const { data: linkedIssue } = useQuery({
    ...issueDetailOptions(wsId, linkedIssueId ?? ""),
    enabled: Boolean(wsId && linkedIssueId),
  });

  const showOriginSeparately = originIssueId && originIssueId !== issueId;
  const showRequesterSeparately =
    requesterUserId &&
    !(authorType === "member" && authorId === requesterUserId);

  return (
    <div className={className}>
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <span className="flex items-center gap-1.5">
          <ActorAvatar actorType={authorType} actorId={authorId} size={18} />
          <span className="font-medium">
            {getActorName(authorType, authorId)}
          </span>
          {authorType === "agent" && (
            <Badge variant="outline" className="text-[10px]">
              agent
            </Badge>
          )}
        </span>
        {showRequesterSeparately && requesterUserId && (
          <span className="flex items-center gap-1.5 text-muted-foreground">
            for{" "}
            <ActorAvatar actorType="member" actorId={requesterUserId} size={16} />
            <span className="font-medium text-foreground">
              {getMemberName(requesterUserId)}
            </span>
          </span>
        )}
        {issueId && (
          <span className="flex items-center gap-1 text-muted-foreground">
            on{" "}
            <a
              href={wsPaths.issueDetail(issueId)}
              className="underline hover:text-foreground"
            >
              {linkedIssue?.identifier ?? "issue"}
              {linkedIssue?.title ? ` — ${linkedIssue.title}` : ""}
            </a>
          </span>
        )}
        {projectId && (
          <span className="flex items-center gap-1 text-muted-foreground">
            on{" "}
            <a
              href={wsPaths.projectDetail(projectId)}
              className="underline hover:text-foreground"
            >
              project
            </a>
          </span>
        )}
        {showOriginSeparately && originIssueId && (
          <span className="flex items-center gap-1 text-muted-foreground">
            from{" "}
            <a
              href={wsPaths.issueDetail(originIssueId)}
              className="underline hover:text-foreground"
            >
              {linkedIssue?.identifier ?? "issue"}
            </a>
          </span>
        )}
      </div>
      {updatedAt && (
        <p className="mt-1 text-xs text-muted-foreground">
          Updated {formatDateTime(updatedAt)}
        </p>
      )}
    </div>
  );
}

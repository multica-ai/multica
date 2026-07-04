import { useQuery } from "@tanstack/react-query";
import { listIssueTypesOptions } from "@multica/core/issue-types/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { IssueTypeIcon } from "./issue-type-icon";
import type { IssueType } from "@multica/core/types";

export function IssueTypeBadge({
  issueTypeId,
  showName = false,
  className = "",
}: {
  issueTypeId: string | null;
  showName?: boolean;
  className?: string;
}) {
  const wsId = useWorkspaceId();
  const { data = [] } = useQuery(listIssueTypesOptions(wsId));
  const issueTypes = data as IssueType[];

  if (!issueTypeId) return null;

  const issueType = issueTypes.find((t: IssueType) => t.id === issueTypeId);
  if (!issueType) return null;

  return (
    <span
      className={`inline-flex items-center gap-1 text-xs text-muted-foreground ${className}`}
      title={issueType.name}
    >
      <IssueTypeIcon
        icon={issueType.icon}
        color={issueType.color}
        className="h-3 w-3 shrink-0"
      />
      {showName && <span className="truncate">{issueType.name}</span>}
    </span>
  );
}

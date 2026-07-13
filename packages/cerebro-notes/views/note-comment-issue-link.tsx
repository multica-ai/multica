"use client";

// NoteCommentIssueLink (FIR-3102) — the "opened <issue>" chip shown on a note
// comment that has been turned into a standalone issue (comment.issue_id). It
// resolves the issue's identifier (e.g. MUL-123) for the label and navigates to
// the issue on click, so a comment's issue can be reopened straight from the
// margin. While the identifier loads (or if the issue can't be read) it falls
// back to a neutral "Issue" label — the chip still links by id.
import { useQuery } from "@tanstack/react-query";
import { ListTodo } from "lucide-react";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";

export function NoteCommentIssueLink({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: Boolean(wsId && issueId),
  });
  const label = issue?.identifier || "Issue";

  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        navigation.push(paths.issueDetail(issueId));
      }}
      title={issue?.title ? `${label} · ${issue.title}` : `Open ${label}`}
      className="mt-1.5 inline-flex max-w-full items-center gap-1 rounded-full border border-border bg-muted/40 px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
    >
      <ListTodo className="size-3 shrink-0" />
      <span className="shrink-0">Opened</span>
      <span className="truncate font-medium text-foreground">{label}</span>
    </button>
  );
}

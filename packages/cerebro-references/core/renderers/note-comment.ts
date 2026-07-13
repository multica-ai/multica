import { registerObjectRenderer } from "../registry";
import type { IssueReference } from "../types";

function metadataString(ref: IssueReference, key: string): string {
  const value = ref.metadata?.[key];
  return typeof value === "string" ? value : "";
}

export function formatNoteCommentTitle(ref: IssueReference): string {
  return ref.label?.trim() || "Source note comment";
}

export function resolveNoteCommentUrl(ref: IssueReference): string | null {
  const noteId = metadataString(ref, "note_id");
  const commentId = metadataString(ref, "comment_id") || ref.ref_id;
  if (!noteId || !commentId) return null;

  let workspace = "";
  if (typeof window !== "undefined") {
    workspace = window.location.pathname.split("/").filter(Boolean)[0] ?? "";
  }
  if (!workspace) return null;
  const query = new URLSearchParams({ comment: commentId });
  return `/${encodeURIComponent(workspace)}/notes/${encodeURIComponent(noteId)}?${query.toString()}`;
}

registerObjectRenderer({
  object: "note_comment",
  formatTitle: formatNoteCommentTitle,
  formatBadge: () => "Note comment",
  resolveUrl: resolveNoteCommentUrl,
});

import type { TimelineEntry } from "@multica/core/types";

export function commentContentFromTimeline(
  timeline: TimelineEntry[] | undefined,
  commentId: string,
): string | undefined {
  return timeline?.find(
    (entry) => entry.type === "comment" && entry.id === commentId,
  )?.content;
}

export function buildCommentUpdateBody(
  content: string,
  attachmentIds: string[] | undefined,
  contentBase: string | undefined,
) {
  return {
    content,
    ...(attachmentIds ? { attachment_ids: attachmentIds } : {}),
    ...(contentBase !== undefined ? { content_base: contentBase } : {}),
  };
}

export function shouldAcceptServerRevision(
  currentRevision: number | undefined,
  incomingRevision: number | undefined,
): boolean {
  return (
    currentRevision === undefined ||
    (incomingRevision !== undefined && incomingRevision > currentRevision)
  );
}

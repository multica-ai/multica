import type { TimelineEntry, UpdateIssueRequest } from "@multica/core/types";

export function withCachedIssueRevision(
  patch: UpdateIssueRequest,
  cachedRevision: number | undefined,
): UpdateIssueRequest {
  return patch.expected_revision === undefined && cachedRevision !== undefined
    ? { ...patch, expected_revision: cachedRevision }
    : patch;
}

export function commentRevisionFromTimeline(
  timeline: TimelineEntry[] | undefined,
  commentId: string,
): number | undefined {
  return timeline?.find(
    (entry) => entry.type === "comment" && entry.id === commentId,
  )?.revision;
}

export function buildCommentUpdateBody(
  content: string,
  attachmentIds: string[] | undefined,
  expectedRevision: number | undefined,
) {
  return {
    content,
    ...(attachmentIds ? { attachment_ids: attachmentIds } : {}),
    ...(expectedRevision !== undefined
      ? { expected_revision: expectedRevision }
      : {}),
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

"use client";

/**
 * Shared React Query for fetching attachment text bodies via the
 * `/api/attachments/{id}/content` proxy.
 *
 * Same retry / staleTime / gcTime policy as AttachmentPreviewModal's local
 * TextBackedPreview, lifted out so the modal and the inline `AttachmentCard`
 * (file-card NodeView / readonly file-card / standalone AttachmentList) hit
 * the same cache key — opening the modal after the inline preview already
 * loaded a body does not refetch.
 */

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";

export function useAttachmentHtmlText(attachmentId: string | null | undefined) {
  return useQuery({
    queryKey: ["attachment-content", attachmentId ?? ""] as const,
    queryFn: () => api.getAttachmentTextContent(attachmentId as string),
    enabled: !!attachmentId,
    // 413 / 415 won't become 200 on retry; a transport error is easier to
    // recover from by re-opening than waiting on background retries with
    // no UI affordance.
    retry: false,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
  });
}

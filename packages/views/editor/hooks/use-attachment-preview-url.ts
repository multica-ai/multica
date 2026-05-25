"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";

export function useAttachmentPreviewURL(attachmentId: string | null | undefined) {
  return useQuery({
    queryKey: ["attachment-preview-url", attachmentId ?? ""] as const,
    queryFn: () => api.getAttachmentPreviewURL(attachmentId as string),
    enabled: !!attachmentId,
    retry: false,
    staleTime: 20 * 60_000,
    gcTime: 30 * 60_000,
  });
}

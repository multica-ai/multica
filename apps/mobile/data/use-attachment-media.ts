import { useQuery } from "@tanstack/react-query";
import { selectedAttachmentId } from "@/lib/attachment-source";
import { freshAttachmentUrl } from "./attachment-media";

export function useAttachmentMedia(source: string) {
  const id = selectedAttachmentId(source);
  const query = useQuery({
    queryKey: ["attachment-media", id],
    queryFn: ({ signal }) => freshAttachmentUrl(source, false, signal),
    enabled: !!id,
    // Proxy capabilities expire after 60s. Refresh on reuse and foreground;
    // already-decoded images do not need a timer or another byte download.
    staleTime: 30_000,
    refetchOnMount: "always",
    refetchOnWindowFocus: "always",
  });
  return { uri: id ? query.data : source, refetch: query.refetch };
}

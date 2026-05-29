import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChannelDetail } from "@multica/views/channels";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelDetailOptions } from "@multica/core/channels";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function ChannelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: channel } = useQuery(channelDetailOptions(wsId, id!));

  useDocumentTitle(channel?.title ?? "Channel");

  if (!id) return null;
  return (
    <ErrorBoundary resetKeys={[id]}>
      <ChannelDetail channelId={id} initialChannel={channel} />
    </ErrorBoundary>
  );
}

import { useParams } from "react-router-dom";
import { ChannelList, ChannelMessages, ChannelComposer } from "@multica/views/channels";

export function ChannelDetailPage() {
  const { channelId } = useParams<{ channelId: string }>();

  if (!channelId) {
    return (
      <div className="flex h-full">
        <div className="w-64 shrink-0 border-r">
          <ChannelList />
        </div>
        <div className="flex-1 flex items-center justify-center text-muted-foreground">
          频道未找到
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full">
      <div className="w-64 shrink-0 border-r">
        <ChannelList />
      </div>
      <div className="flex-1 flex flex-col">
        <div className="flex-1 overflow-auto">
          <ChannelMessages channelId={channelId} />
        </div>
        <ChannelComposer channelId={channelId} />
      </div>
    </div>
  );
}

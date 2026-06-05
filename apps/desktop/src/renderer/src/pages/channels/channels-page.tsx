import { ChannelList } from "@multica/views/channels";

export function ChannelsPage() {
  return (
    <div className="flex h-full">
      <div className="w-64 shrink-0 border-r">
        <ChannelList />
      </div>
      <div className="flex-1 flex items-center justify-center text-muted-foreground">
        选择一个频道开始讨论
      </div>
    </div>
  );
}

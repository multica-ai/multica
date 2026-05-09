"use client";

import { ChannelList } from "@multica/views/channels";
import { Hash } from "lucide-react";

export default function ChannelsPage() {
  return (
    <div className="flex h-full">
      <div className="w-56 shrink-0 border-r overflow-y-auto">
        <ChannelList />
      </div>
      <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
        <Hash className="size-10 opacity-20" />
        <p className="text-sm">选择一个频道开始讨论</p>
      </div>
    </div>
  );
}

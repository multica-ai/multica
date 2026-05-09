"use client";

import { use } from "react";
import { ChannelDetail } from "@multica/views/channels";

export default function ChannelDetailPage({
  params,
}: {
  params: Promise<{ channelId: string }>;
}) {
  const { channelId } = use(params);
  return <ChannelDetail channelId={channelId} />;
}

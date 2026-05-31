"use client";

import { use } from "react";

import { ChannelsPage } from "@multica/views/channels";

export default function ChannelDetailRoutePage({
  params,
}: {
  params: Promise<{ channelId: string }>;
}) {
  const { channelId } = use(params);
  return <ChannelsPage channelId={decodeURIComponent(channelId)} />;
}

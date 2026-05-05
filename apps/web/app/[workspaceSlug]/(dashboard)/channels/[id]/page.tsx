"use client";

import { use } from "react";
import { ChannelsPage } from "@multica/views/channels";

export default function ChannelDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <ChannelsPage activeChannelId={id} />;
}

"use client";

import { use } from "react";
import { ChannelDetail } from "@multica/views/channels";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function ChannelDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <ChannelDetail channelId={id} />
    </ErrorBoundary>
  );
}

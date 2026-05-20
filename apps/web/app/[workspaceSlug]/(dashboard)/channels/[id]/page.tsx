"use client";

import { use } from "react";
import { IssueDetail } from "@multica/views/issues/components/issue-detail";

// Channels and DMs share the issue detail component — the underlying entity
// is an issue with kind 'channel' | 'dm'. The component branches on
// issue.kind to swap chrome (no status pill, participant stack in header,
// etc.) and renders the same comment/message stream.
export default function ChannelDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <IssueDetail issueId={id} />;
}

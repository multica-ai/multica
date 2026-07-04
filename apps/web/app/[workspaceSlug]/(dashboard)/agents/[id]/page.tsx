"use client";

import { use } from "react";
import { AgentDetailPage } from "@multica/views/agents";
// FIR-2670: opt-in preview of the redesigned agent detail page.
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { CerebroAgentDetailPage } from "@multica/cerebro-agent-page-redesign/views";

export default function AgentDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  // FIR-2670: when the preview flag is on, render the redesigned page; the
  // current page stays the default until the redesign is verified live.
  const redesign = useFeatureFlag("cerebro_agent_page_redesign");
  if (redesign) {
    return <CerebroAgentDetailPage agentId={id} />;
  }
  return <AgentDetailPage agentId={id} />;
}

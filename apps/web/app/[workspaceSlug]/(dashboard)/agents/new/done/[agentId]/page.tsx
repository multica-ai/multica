"use client";

import { use } from "react";
import { AgentCreatedPage } from "@multica/views/agents/agent-created-page";

export default function NewAgentDoneRoute({
  params,
}: {
  params: Promise<{ agentId: string }>;
}) {
  const { agentId } = use(params);
  return <AgentCreatedPage agentId={agentId} />;
}

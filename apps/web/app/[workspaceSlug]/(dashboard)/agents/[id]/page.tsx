"use client";

import { use } from "react";
import { AgentDetailRoute } from "@multica/views/agents";

export default function AgentDetailRoutePage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <AgentDetailRoute agentId={id} />;
}

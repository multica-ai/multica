"use client";

import { use } from "react";
import { AiBuilderSessionPage } from "@multica/views/agents/ai-builder-session-page";

export default function NewAgentAiSessionRoute({
  params,
}: {
  params: Promise<{ sessionId: string }>;
}) {
  const { sessionId } = use(params);
  return <AiBuilderSessionPage sessionId={sessionId} />;
}

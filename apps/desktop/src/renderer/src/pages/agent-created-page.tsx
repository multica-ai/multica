import { useParams } from "react-router-dom";
import { AgentCreatedPage as SharedAgentCreatedPage } from "@multica/views/agents";

export function AgentCreatedPage() {
  const { agentId } = useParams<{ agentId: string }>();
  if (!agentId) return null;
  return <SharedAgentCreatedPage agentId={agentId} />;
}

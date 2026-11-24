import { useParams } from "react-router-dom";
import { AgentDetailPage as SharedAgentDetailPage } from "@multica/views/agents";

export function AgentDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <SharedAgentDetailPage agentId={id} />;
}

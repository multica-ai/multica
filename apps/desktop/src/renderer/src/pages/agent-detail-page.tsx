import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AgentDetailPage as SharedAgentDetailPage } from "@multica/views/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useT } from "@multica/i18n/react";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AgentDetailPage() {
  const t = useT("desktop");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agent = agents.find((a) => a.id === id) ?? null;

  useDocumentTitle(agent?.name ?? t("page_agent"));

  if (!id) return null;
  return <SharedAgentDetailPage agentId={id} />;
}

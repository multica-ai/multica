import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AutopilotDetailPage as AutopilotDetail } from "@wallts/views/autopilots/components";
import { useWorkspaceId } from "@wallts/core/hooks";
import { autopilotDetailOptions } from "@wallts/core/autopilots/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AutopilotDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data } = useQuery(autopilotDetailOptions(wsId, id!));

  useDocumentTitle(data ? `⚡ ${data.autopilot.title}` : "Autopilot");

  if (!id) return null;
  return <AutopilotDetail autopilotId={id} />;
}

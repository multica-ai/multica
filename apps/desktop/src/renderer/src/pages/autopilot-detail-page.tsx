import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AutopilotDetailPage as AutopilotDetail } from "@multica/views/autopilots/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { autopilotDetailOptions } from "@multica/core/autopilots/queries";
import { useT } from "@multica/i18n/react";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AutopilotDetailPage() {
  const t = useT("desktop");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data } = useQuery(autopilotDetailOptions(wsId, id!));

  useDocumentTitle(data ? `⚡ ${data.autopilot.title}` : t("page_autopilot"));

  if (!id) return null;
  return <AutopilotDetail autopilotId={id} />;
}

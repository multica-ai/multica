import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AutopilotDetailPage as AutopilotDetail } from "@multica/views/autopilots/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { autopilotDetailOptions } from "@multica/core/autopilots/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";
import { useT } from "@multica/views/i18n";

export function AutopilotDetailPage() {
  const { t } = useT("layout");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data } = useQuery(autopilotDetailOptions(wsId, id!));

  // Plain text only — no leading ⚡ glyph in the title (MUL-4370).
  useDocumentTitle(data ? data.autopilot.title : t(($) => $.tab.autopilot));

  if (!id) return null;
  return <AutopilotDetail autopilotId={id} />;
}

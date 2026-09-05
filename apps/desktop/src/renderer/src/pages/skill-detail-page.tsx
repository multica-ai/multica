import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SkillDetailPage as SharedSkillDetailPage } from "@multica/views/skills";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";
import { useT } from "@multica/views/i18n";

export function SkillDetailPage() {
  const { t } = useT("layout");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: skill } = useQuery(skillDetailOptions(wsId, id ?? ""));

  useDocumentTitle(skill?.name ?? t(($) => $.tab.skill));

  if (!id) return null;
  return <SharedSkillDetailPage skillId={id} />;
}

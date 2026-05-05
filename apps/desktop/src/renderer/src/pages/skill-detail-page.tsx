import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SkillDetailPage as SharedSkillDetailPage } from "@multica/views/skills";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { useT } from "@multica/i18n/react";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function SkillDetailPage() {
  const t = useT("desktop");
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: skill } = useQuery(skillDetailOptions(wsId, id ?? ""));

  useDocumentTitle(skill?.name ?? t("page_skill"));

  if (!id) return null;
  return <SharedSkillDetailPage skillId={id} />;
}

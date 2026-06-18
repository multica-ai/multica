import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
// CEREBRO-PATCH(skills-direct-exports): import the detail view directly so the skills list entrypoint stays split.
import { SkillDetailPage as SharedSkillDetailPage } from "@multica/views/skills/components/skill-detail-page";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function SkillDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: skill } = useQuery(skillDetailOptions(wsId, id ?? ""));

  useDocumentTitle(skill?.name ?? "Skill");

  if (!id) return null;
  return <SharedSkillDetailPage skillId={id} />;
}

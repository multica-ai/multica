import { useParams } from "react-router-dom";
import { SkillDetailPage as SharedSkillDetailPage } from "@multica/views/skills";

export function SkillDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <SharedSkillDetailPage skillId={id} />;
}

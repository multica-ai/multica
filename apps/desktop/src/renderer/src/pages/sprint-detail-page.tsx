import { useParams } from "react-router-dom";
import { SprintBoard } from "@multica/views/projects/components";

export function SprintDetailPage() {
  const { sprintId } = useParams<{ sprintId: string }>();
  if (!sprintId) return null;
  return <SprintBoard sprintId={sprintId} />;
}

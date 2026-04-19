import { useParams } from "react-router-dom";
import { RuntimeGroupDetailPage } from "@multica/views/runtime-groups/runtime-group-detail-page";

export function RuntimeGroupDetailPageRoute() {
  const { groupId } = useParams<{ groupId: string }>();
  if (!groupId) return null;
  return <RuntimeGroupDetailPage groupId={groupId} />;
}

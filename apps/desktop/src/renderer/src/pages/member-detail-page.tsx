import { useParams } from "react-router-dom";
import { MemberDetailPage as SharedMemberDetailPage } from "@multica/views/members";

export function MemberDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <SharedMemberDetailPage userId={id} />;
}

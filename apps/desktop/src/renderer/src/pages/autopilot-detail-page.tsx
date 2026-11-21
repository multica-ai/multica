import { useParams } from "react-router-dom";
import { AutopilotDetailPage as AutopilotDetail } from "@multica/views/autopilots/components";

export function AutopilotDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <AutopilotDetail autopilotId={id} />;
}

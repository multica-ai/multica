import { useParams } from "react-router-dom";
import { OneShotRunPage as OneShotRun } from "@multica/views/oneshot";

export function OneShotRunPage() {
  const { id } = useParams<{ id: string }>();
  return id ? <OneShotRun taskId={id} /> : null;
}

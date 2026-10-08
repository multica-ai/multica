import { useParams } from "react-router-dom";
import { AiBuilderSessionPage as SharedAiBuilderSessionPage } from "@multica/views/agents";

export function AiBuilderSessionPage() {
  const { sessionId } = useParams<{ sessionId: string }>();
  if (!sessionId) return null;
  return <SharedAiBuilderSessionPage sessionId={sessionId} />;
}

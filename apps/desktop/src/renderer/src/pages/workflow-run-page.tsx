import { useParams } from "react-router-dom";
import { WorkflowRunPage as WorkflowRun } from "@multica/views/workflows/components";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowRunPage() {
  const { id, runId } = useParams<{ id: string; runId: string }>();

  useDocumentTitle("Workflow Run");

  if (!id || !runId) return null;
  return <WorkflowRun workflowId={id} runId={runId} />;
}

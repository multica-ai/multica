import { useParams } from "react-router-dom";
import { WorkflowRunsPage as WorkflowRuns } from "@multica/views/workflows/components";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowRunsPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Workflow Runs");

  if (!id) return null;
  return <WorkflowRuns workflowId={id} />;
}

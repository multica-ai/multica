import { useParams } from "react-router-dom";
import { WorkflowOverviewPage as WorkflowOverview } from "@multica/views/workflows/components/overview";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowOverviewPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Workflow Overview");

  if (!id) return null;
  return <WorkflowOverview workflowId={id} />;
}

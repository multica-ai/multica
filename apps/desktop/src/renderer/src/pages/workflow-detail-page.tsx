import { useParams } from "react-router-dom";
import { WorkflowDetailPage as WorkflowDetail } from "@multica/views/workflows/components";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowDetailPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Workflow");

  if (!id) return null;
  return <WorkflowDetail workflowId={id} />;
}

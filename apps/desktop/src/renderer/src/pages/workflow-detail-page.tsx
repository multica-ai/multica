import { useParams } from "react-router-dom";
import { WorkflowDetailShell } from "@multica/views/workflows/components";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowDetailPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Workflow");

  if (!id) return null;
  return <WorkflowDetailShell workflowId={id} />;
}

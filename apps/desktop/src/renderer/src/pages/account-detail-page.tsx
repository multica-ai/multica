import { useParams } from "react-router-dom";
import { AccountDetailPage as SharedAccountDetailPage } from "@multica/cerebro-runtime/views";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AccountDetailPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Account");

  if (!id) return null;
  return <SharedAccountDetailPage accountId={id} />;
}

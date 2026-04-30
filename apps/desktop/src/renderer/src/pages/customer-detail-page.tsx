import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CustomerDetail } from "@multica/views/customers/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { customerDetailOptions } from "@multica/core/customers/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function CustomerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: customer } = useQuery(customerDetailOptions(wsId, id!));

  useDocumentTitle(customer ? customer.name : "Customer");

  if (!id) return null;
  return <CustomerDetail customerId={id} />;
}

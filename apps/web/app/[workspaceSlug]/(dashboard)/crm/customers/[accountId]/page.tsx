"use client";

import { useParams } from "next/navigation";
import { CRMAccountDetailPage } from "@multica/views/crm/components";

export default function Page() {
  const params = useParams<{ accountId: string }>();
  return <CRMAccountDetailPage accountId={params.accountId} />;
}

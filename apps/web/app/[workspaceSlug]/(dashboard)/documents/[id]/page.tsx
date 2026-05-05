"use client";

import { useParams } from "next/navigation";
import { DocumentViewPage } from "@multica/cerebro-artifacts/views/pages";

export default function Page() {
  const params = useParams<{ id: string }>();
  return <DocumentViewPage artifactId={params.id} />;
}

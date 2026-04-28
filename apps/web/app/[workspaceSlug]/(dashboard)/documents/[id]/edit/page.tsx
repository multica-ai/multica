"use client";

import { useParams } from "next/navigation";
import { DocumentEditPage } from "@multica/views/artifacts/pages";

export default function Page() {
  const params = useParams<{ id: string }>();
  return <DocumentEditPage artifactId={params.id} />;
}

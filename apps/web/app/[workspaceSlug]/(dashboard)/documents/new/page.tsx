"use client";

import { useSearchParams } from "next/navigation";
import { DocumentNewPage } from "@multica/views/artifacts/pages";

export default function Page() {
  const params = useSearchParams();
  return <DocumentNewPage folderId={params.get("folder")} />;
}

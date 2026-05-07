"use client";

import { useSearchParams } from "next/navigation";
import { DocumentNewPage } from "@multica/cerebro-artifacts/views/pages";

export default function Page() {
  const params = useSearchParams();
  return <DocumentNewPage folderId={params.get("folder")} />;
}

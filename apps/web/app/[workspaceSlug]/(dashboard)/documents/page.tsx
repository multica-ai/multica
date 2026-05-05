"use client";

import { useSearchParams } from "next/navigation";
import { FileManagerPage } from "@multica/cerebro-artifacts/views/components";

export default function Page() {
  const params = useSearchParams();
  return <FileManagerPage initialFolderId={params.get("folder")} />;
}

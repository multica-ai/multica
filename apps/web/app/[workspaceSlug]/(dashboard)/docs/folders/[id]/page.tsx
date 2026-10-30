"use client";

import { use } from "react";
import { DocFolderPage } from "@multica/views/docs";

export default function DocFolderRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <DocFolderPage folderId={id} />;
}

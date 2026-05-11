"use client";

import { use } from "react";
import { WikiPage } from "@multica/views/wiki";

export default function WikiDetailPage({
  params,
}: {
  params: Promise<{ pageId: string }>;
}) {
  const { pageId } = use(params);
  return <WikiPage pageId={pageId} />;
}

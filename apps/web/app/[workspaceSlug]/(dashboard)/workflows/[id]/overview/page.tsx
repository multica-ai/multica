"use client";

import { useEffect } from "react";
import { use } from "react";
import { useRouter } from "next/navigation";
import { useWorkspacePaths } from "@multica/core/paths";

/** Redirects to /workflows/[id] (overview is now the default view). */
export default function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const router = useRouter();
  const wsPaths = useWorkspacePaths();
  useEffect(() => {
    router.replace(wsPaths.workflowDetail(id));
  }, [id, router, wsPaths]);
  return null;
}

"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";

export default function Page() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return (
    <NewWorkspacePage
      onSuccess={(ws) => router.push(paths.workspace(ws.slug).issues())}
    />
  );
}

"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { paths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { getPreferredStartPage } from "@multica/cerebro-preferences/views";

function workspaceStartPagePath(workspaceSlug: string, page: string | null) {
  if (!page) return paths.workspace(workspaceSlug).issues();
  return `/${encodeURIComponent(workspaceSlug)}/${page}`;
}

export function WorkspaceRootRedirect({
  workspaceSlug,
}: {
  workspaceSlug: string;
}) {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (isAuthLoading || !user) return;
    const isMobile =
      typeof window !== "undefined" && window.innerWidth < 768;
    const page = getPreferredStartPage(user, isMobile ? "mobile" : "desktop");
    router.replace(workspaceStartPagePath(workspaceSlug, page));
  }, [isAuthLoading, router, user, workspaceSlug]);

  return null;
}

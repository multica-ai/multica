"use client";

// Workspace root redirect.
//
// Without this, `/{workspaceSlug}` hits Next's default 404 because the
// dashboard has no index page (every meaningful surface is at a named
// child route — /issues, /inbox, /ship, etc.). A user clicking a stale
// bookmark, the breadcrumb, or just typing the workspace slug in the
// URL bar would land on the unhelpful Next 404. Redirecting to /inbox
// is the closest we have to a "home" — it's where unread mentions and
// new assignments surface, the natural landing for "what should I look
// at next?".
//
// Client redirect (not a Next.js redirect()) because the workspace
// layout is also a client component and resolves `workspaceSlug` from a
// promise; doing this server-side would require duplicating workspace
// auth + slug resolution.

import { use, useEffect } from "react";
import { useRouter } from "next/navigation";
import { paths } from "@multica/core/paths";

export default function WorkspaceIndex({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const router = useRouter();
  useEffect(() => {
    router.replace(paths.workspace(workspaceSlug).inbox());
  }, [workspaceSlug, router]);
  return null;
}

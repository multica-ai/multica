"use client";

import { useEffect } from "react";
import { useRouter, useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { InvitePage } from "@multica/views/invite";

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const authStatus = useAuthStore((s) => s.authStatus);
  const authTemporarilyUnavailable =
    authStatus === "temporarily_unreachable";
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  // Redirect to login if not authenticated, with a redirect back to this page.
  useEffect(() => {
    if (!isLoading && !authTemporarilyUnavailable && !user) {
      router.replace(
        `${paths.login()}?next=${encodeURIComponent(paths.invite(params.id))}`,
      );
    }
  }, [isLoading, authTemporarilyUnavailable, user, router, params.id]);

  if (isLoading || authTemporarilyUnavailable || !user) return null;

  const onBack =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return <InvitePage invitationId={params.id} onBack={onBack} />;
}

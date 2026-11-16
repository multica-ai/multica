"use client";

import { useEffect } from "react";
import { useRouter, useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { InvitationsPage } from "@multica/views/invitations";

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  // Redirect to login if not authenticated, with a redirect back to this page.
  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(
        `${paths.login()}?next=${encodeURIComponent(paths.invite(params.id))}`,
      );
    }
  }, [isLoading, user, router, params.id]);

  if (isLoading || !user) return null;

  const onBack =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  // The root layout sets `body { overflow: hidden }` (app-shell convention),
  // so the page owns its scroll; otherwise a long invitation list is cut off.
  return (
    <div className="h-full overflow-y-auto bg-background">
      <InvitationsPage invitationId={params.id} onBack={onBack} />
    </div>
  );
}

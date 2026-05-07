"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import {
  myInvitationListOptions,
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";

export default function Page() {
  const router = useRouter();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  // Safety net: a user with pending invitations and no workspace should be
  // offered the invitations wall before being asked to create a workspace —
  // login/callback already do this routing, but a direct hit on this URL
  // (saved tab, manual link) would otherwise skip the check.
  const { data: invites = [], isFetched: invitesFetched } = useQuery({
    ...myInvitationListOptions(),
    enabled: !!user && wsList.length === 0,
  });

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  useEffect(() => {
    if (!user || wsList.length > 0 || !invitesFetched) return;
    if (invites.length > 0) {
      qc.setQueryData(workspaceKeys.myInvitations(), invites);
      router.replace(paths.invitations());
    }
  }, [user, wsList.length, invitesFetched, invites, qc, router]);

  if (isLoading || !user) return null;
  // Hold render while we still don't know whether to surface an invite —
  // avoids a flash of the "Create your workspace" form for invited users.
  if (wsList.length === 0 && !invitesFetched) return null;
  if (wsList.length === 0 && invites.length > 0) return null;

  // Back goes to the root path — the workspace layout redirects from
  // there to the user's default workspace. Only show Back when there's
  // somewhere to go back to (user already has at least one workspace).
  const onBack =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return (
    <NewWorkspacePage
      onSuccess={(ws) => router.push(paths.workspace(ws.slug).issues())}
      onBack={onBack}
    />
  );
}

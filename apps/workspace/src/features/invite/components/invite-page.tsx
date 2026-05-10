import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "@/shared/api";
import type { WorkspaceInviteInfo } from "@/shared/types";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { MulticaIcon } from "@/components/multica-icon";

interface InvitePageProps {
  token: string;
}

/**
 * InvitePage — handles the workspace invite link flow.
 *
 * - Unauthenticated users are redirected to /login?redirect=/invite/:token
 * - Already-members are redirected to /
 * - Valid token with non-member user shows workspace info + Join button
 * - Invalid/disabled token shows an error state
 */
export function InvitePage({ token }: InvitePageProps) {
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const workspaces = useWorkspaceStore((s) => s.workspaces);

  const [inviteInfo, setInviteInfo] = useState<WorkspaceInviteInfo | null>(null);
  const [infoError, setInfoError] = useState(false);
  const [infoLoading, setInfoLoading] = useState(true);
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);

  // Load workspace info from the public endpoint.
  useEffect(() => {
    let cancelled = false;
    setInfoLoading(true);
    api
      .getInviteInfo(token)
      .then((info) => {
        if (!cancelled) {
          setInviteInfo(info);
          setInfoError(false);
        }
      })
      .catch(() => {
        if (!cancelled) setInfoError(true);
      })
      .finally(() => {
        if (!cancelled) setInfoLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [token]);

  // Redirect unauthenticated users to login once auth state is known.
  useEffect(() => {
    if (isAuthLoading) return;
    if (!user) {
      void navigate({ to: "/login", search: { redirect: `/invite/${token}` } });
    }
  }, [isAuthLoading, user, token, navigate]);

  // If the user is already a member of the target workspace, redirect to /.
  useEffect(() => {
    if (!inviteInfo || !user) return;
    const alreadyMember = workspaces.some((ws) => ws.id === inviteInfo.id);
    if (alreadyMember) {
      void navigate({ to: "/" });
    }
  }, [inviteInfo, user, workspaces, navigate]);

  const handleJoin = async () => {
    if (!user) return;
    setJoining(true);
    setJoinError(null);
    try {
      await api.joinByInviteToken(token);
      // Refresh workspace list to pick up the new membership, then navigate.
      await useWorkspaceStore.getState().refreshWorkspaces();
      void navigate({ to: "/" });
    } catch (e) {
      setJoinError(e instanceof Error ? e.message : "Failed to join workspace");
    } finally {
      setJoining(false);
    }
  };

  if (isAuthLoading || infoLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (infoError) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4">
        <MulticaIcon className="size-8" />
        <h1 className="text-lg font-semibold">Invite link not found</h1>
        <p className="text-sm text-muted-foreground max-w-sm text-center">
          This invite link is invalid or has been disabled. Ask a workspace admin for a new link.
        </p>
        <Button variant="outline" onClick={() => void navigate({ to: "/" })}>
          Go to app
        </Button>
      </div>
    );
  }

  if (!inviteInfo) return null;

  return (
    <div className="flex h-screen flex-col items-center justify-center gap-6">
      <MulticaIcon className="size-10" />
      <div className="text-center space-y-1">
        <h1 className="text-xl font-semibold">Join {inviteInfo.name}</h1>
        <p className="text-sm text-muted-foreground">
          You've been invited to join the <span className="font-medium">{inviteInfo.name}</span> workspace.
        </p>
      </div>
      {joinError && <p className="text-sm text-destructive">{joinError}</p>}
      <Button onClick={handleJoin} disabled={joining} className="min-w-32">
        {joining ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            Joining...
          </>
        ) : (
          `Join ${inviteInfo.name}`
        )}
      </Button>
    </div>
  );
}

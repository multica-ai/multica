"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { paths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Users, Check, X } from "lucide-react";

export interface InvitePageProps {
  invitationId: string;
}

export function InvitePage({ invitationId }: InvitePageProps) {
  const { push } = useNavigation();
  const qc = useQueryClient();
  const [accepting, setAccepting] = useState(false);
  const [declining, setDeclining] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<"accepted" | "declined" | null>(null);

  const { data: invitation, isLoading, error: fetchError } = useQuery({
    queryKey: ["invitation", invitationId],
    queryFn: () => api.getInvitation(invitationId),
  });

  // Workspace list for the fallback "Go to dashboard" destinations. The invite
  // page is a pre-workspace global route so we can't rely on WorkspaceSlugProvider.
  const { data: wsList = [] } = useQuery(workspaceListOptions());
  const fallbackDest =
    wsList[0] ? paths.workspace(wsList[0].slug).issues() : paths.newWorkspace();

  const handleAccept = async () => {
    setAccepting(true);
    setError(null);
    try {
      await api.acceptInvitation(invitationId);
      setDone("accepted");
      // Fetch the refreshed workspace list so we know the joined workspace's slug.
      const nextList = await qc.fetchQuery({
        ...workspaceListOptions(),
        staleTime: 0,
      });
      const joined = nextList.find((w) => w.id === invitation?.workspace_id);
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      // Navigate into the joined workspace. The [workspaceSlug]/layout will
      // sync api client, stores, and the last_workspace_slug cookie from the URL.
      const dest = joined
        ? paths.workspace(joined.slug).issues()
        : fallbackDest;
      setTimeout(() => push(dest), 1000);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to accept invitation");
    } finally {
      setAccepting(false);
    }
  };

  const handleDecline = async () => {
    setDeclining(true);
    setError(null);
    try {
      await api.declineInvitation(invitationId);
      setDone("declined");
      qc.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to decline invitation");
    } finally {
      setDeclining(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-sm text-muted-foreground">Loading invitation...</div>
      </div>
    );
  }

  if (fetchError || !invitation) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
              <X className="h-6 w-6 text-muted-foreground" />
            </div>
            <h2 className="text-lg font-semibold">Invitation not found</h2>
            <p className="text-sm text-muted-foreground text-center">
              This invitation may have expired, been revoked, or doesn&apos;t belong to your account.
            </p>
            <Button variant="outline" onClick={() => push(fallbackDest)}>
              Go to dashboard
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (done === "accepted") {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10">
              <Check className="h-6 w-6 text-primary" />
            </div>
            <h2 className="text-lg font-semibold">You joined {invitation.workspace_name}!</h2>
            <p className="text-sm text-muted-foreground">Redirecting to workspace...</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (done === "declined") {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <h2 className="text-lg font-semibold">Invitation declined</h2>
            <p className="text-sm text-muted-foreground">You won&apos;t be added to this workspace.</p>
            <Button variant="outline" onClick={() => push(fallbackDest)}>
              Go to dashboard
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  const isExpired = invitation.status !== "pending";
  const isAlreadyHandled = invitation.status === "accepted" || invitation.status === "declined";

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-md">
        <CardContent className="flex flex-col items-center gap-6 py-12">
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-primary/10">
            <Users className="h-7 w-7 text-primary" />
          </div>

          <div className="text-center space-y-2">
            <h2 className="text-xl font-semibold">
              Join {invitation.workspace_name ?? "workspace"}
            </h2>
            <p className="text-sm text-muted-foreground">
              <strong>{invitation.inviter_name || invitation.inviter_email}</strong>{" "}
              invited you to join as {invitation.role === "admin" ? "an admin" : "a member"}.
            </p>
          </div>

          {isAlreadyHandled ? (
            <div className="text-sm text-muted-foreground">
              This invitation has already been {invitation.status}.
            </div>
          ) : isExpired ? (
            <div className="text-sm text-muted-foreground">
              This invitation has expired.
            </div>
          ) : (
            <div className="flex gap-3 w-full">
              <Button
                variant="outline"
                className="flex-1"
                onClick={handleDecline}
                disabled={accepting || declining}
              >
                {declining ? "Declining..." : "Decline"}
              </Button>
              <Button
                className="flex-1"
                onClick={handleAccept}
                disabled={accepting || declining}
              >
                {accepting ? "Joining..." : "Accept & Join"}
              </Button>
            </div>
          )}

          {error && (
            <p className="text-sm text-destructive text-center">{error}</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

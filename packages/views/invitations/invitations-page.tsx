"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  myInvitationListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import type { Invitation } from "@multica/core/types";
import { AppLink } from "../navigation";
import { useLogout } from "../auth";
import { DragStrip } from "../platform";
import { useT } from "../i18n";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { useInvitationActions } from "./use-invitation-actions";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ArrowLeft, LogOut, Mail, Users } from "@multica/ui/icons";

export interface InvitationsPageProps {
  /**
   * The invitation the user followed a link for (`/invite/:id`). It is pinned
   * to the top and highlighted; when it can no longer be joined, the page
   * says why above the remaining invitations.
   */
  invitationId?: string;
  /**
   * Optional "go back" handler. Callers pass it only when there's somewhere
   * to go (the user has a workspace). Otherwise Log out is the only exit.
   */
  onBack?: () => void;
}

/**
 * The one place to answer workspace invitations before entering a
 * workspace. Shared between web (`/invitations`, `/invite/[id]`) and the
 * desktop window overlay.
 *
 * Each invitation is joined or declined on its own row. Joining navigates
 * into that workspace; the rest stay pending and remain reachable from the
 * workspace switcher. Un-onboarded users also get a way to skip to
 * onboarding — leaving this page never declines anything.
 */
export function InvitationsPage({ invitationId, onBack }: InvitationsPageProps) {
  const { t } = useT("invite");
  const hasOnboarded = useHasOnboarded();
  const { accept, decline } = useInvitationActions();
  const [error, setError] = useState<string | null>(null);

  const { data: pending = [], isLoading } = useQuery(myInvitationListOptions());
  // Read once on arrival so the notice explains why a linked invitation is
  // missing from the list; declining it here must not turn into "already
  // declined", so this query is never invalidated by the actions.
  const linked = useQuery({
    queryKey: ["invitation", invitationId],
    queryFn: () => api.getInvitation(invitationId!),
    enabled: !!invitationId,
    staleTime: 0,
  });
  const { data: workspaces = [] } = useQuery(workspaceListOptions());

  if (isLoading || linked.isLoading) {
    return (
      <InvitationsShell onBack={onBack}>
        <Card className="w-full max-w-lg">
          <CardContent className="flex flex-col gap-4 py-12">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-72" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </CardContent>
        </Card>
      </InvitationsShell>
    );
  }

  const highlighted = pending.find((inv) => inv.id === invitationId);
  const rows = highlighted
    ? [highlighted, ...pending.filter((inv) => inv.id !== invitationId)]
    : pending;
  const notice =
    invitationId && !highlighted ? linkedNotice(linked.data, linked.isError) : null;
  const busy = accept.isPending || decline.isPending;

  const handleAccept = (invitation: Invitation) => {
    setError(null);
    accept.mutate(invitation, {
      onError: (e) => setError(e instanceof Error ? e.message : t(($) => $.errors.accept_failed)),
    });
  };
  const handleDecline = (invitation: Invitation) => {
    setError(null);
    decline.mutate(invitation, {
      onError: (e) => setError(e instanceof Error ? e.message : t(($) => $.errors.decline_failed)),
    });
  };

  if (rows.length === 0) {
    return (
      <InvitationsShell onBack={onBack}>
        <Card className="w-full max-w-md">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
              <Mail className="h-6 w-6 text-muted-foreground" />
            </div>
            <h2 className="text-title font-semibold">{t(($) => $.empty.title)}</h2>
            {(notice || !hasOnboarded) && (
              <p className="text-body text-muted-foreground text-center">
                {notice ? t(($) => $.linked[notice]) : t(($) => $.empty.hint)}
              </p>
            )}
            <Button
              variant={hasOnboarded ? "outline" : "default"}
              render={<AppLink href={resolvePostAuthDestination(workspaces, hasOnboarded)} />}
              nativeButton={false}
            >
              {hasOnboarded ? t(($) => $.empty.go_to_dashboard) : t(($) => $.empty.continue)}
            </Button>
            {error && <p className="text-body text-destructive text-center">{error}</p>}
          </CardContent>
        </Card>
      </InvitationsShell>
    );
  }

  return (
    <InvitationsShell onBack={onBack}>
      <Card className="w-full max-w-lg">
        <CardContent className="flex flex-col gap-6 py-10">
          <div className="flex flex-col items-center gap-3 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10">
              <Users className="h-6 w-6 text-primary" />
            </div>
            <div className="space-y-1">
              <h2 className="text-title-lg font-semibold">{t(($) => $.list.title)}</h2>
              {rows.length > 1 && (
                <p className="text-body text-muted-foreground">{t(($) => $.list.subtitle)}</p>
              )}
            </div>
          </div>

          {notice && (
            <p className="rounded-md bg-muted px-4 py-3 text-body text-muted-foreground">
              {t(($) => $.linked[notice])}
            </p>
          )}

          <ul className="flex flex-col gap-2">
            {rows.map((inv) => (
              <InvitationRow
                key={inv.id}
                invitation={inv}
                highlighted={inv === highlighted}
                primary={!highlighted || inv === highlighted}
                disabled={busy}
                joining={accept.isPending && accept.variables?.id === inv.id}
                declining={decline.isPending && decline.variables?.id === inv.id}
                onAccept={() => handleAccept(inv)}
                onDecline={() => handleDecline(inv)}
              />
            ))}
          </ul>

          {!hasOnboarded && (
            <AppLink
              href={paths.onboarding()}
              className="self-center text-body text-muted-foreground hover:text-foreground"
            >
              {t(($) => $.list.skip)}
            </AppLink>
          )}

          {error && <p className="text-body text-destructive text-center">{error}</p>}
        </CardContent>
      </Card>
    </InvitationsShell>
  );
}

type LinkedNotice = "not_found" | "accepted" | "declined" | "expired";

/**
 * Why a linked invitation is not among the pending ones. A still-pending,
 * unexpired invitation gets no notice: it was just declined on this page.
 */
function linkedNotice(invitation: Invitation | undefined, failed: boolean): LinkedNotice | null {
  if (failed || !invitation) return "not_found";
  if (invitation.status === "accepted" || invitation.status === "declined") {
    return invitation.status;
  }
  if (invitation.status !== "pending" || Date.parse(invitation.expires_at) <= Date.now()) {
    return "expired";
  }
  return null;
}

function InvitationRow({
  invitation,
  highlighted,
  primary,
  disabled,
  joining,
  declining,
  onAccept,
  onDecline,
}: {
  invitation: Invitation;
  highlighted: boolean;
  primary: boolean;
  disabled: boolean;
  joining: boolean;
  declining: boolean;
  onAccept: () => void;
  onDecline: () => void;
}) {
  const { t } = useT("invite");
  const name = invitation.workspace_name ?? t(($) => $.list.workspace_fallback);
  const inviter =
    invitation.inviter_name || invitation.inviter_email || t(($) => $.list.inviter_fallback);
  return (
    <li
      aria-current={highlighted ? "true" : undefined}
      className={cn(
        "flex items-center gap-3 rounded-md border p-4",
        highlighted ? "border-brand/50 bg-brand/5" : "border-border bg-card",
      )}
    >
      <WorkspaceAvatar name={name} size="lg" className="rounded-md" />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="truncate font-medium">{name}</div>
        <div className="truncate text-caption text-muted-foreground">
          {invitation.role === "admin"
            ? t(($) => $.list.invited_admin, { inviter })
            : t(($) => $.list.invited_member, { inviter })}
        </div>
      </div>
      <Button variant="ghost" size="sm" onClick={onDecline} disabled={disabled} aria-busy={declining}>
        {declining ? t(($) => $.list.declining) : t(($) => $.list.decline)}
      </Button>
      <Button
        variant={primary ? "default" : "outline"}
        size="sm"
        onClick={onAccept}
        disabled={disabled}
        aria-busy={joining}
      >
        {joining ? t(($) => $.list.joining) : t(($) => $.list.accept)}
      </Button>
    </li>
  );
}

function InvitationsShell({
  onBack,
  children,
}: {
  onBack?: () => void;
  children: ReactNode;
}) {
  const { t } = useT("invite");
  const logout = useLogout();
  return (
    <div className="relative flex min-h-svh flex-col bg-background">
      <DragStrip />
      {onBack && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-16 left-12 text-muted-foreground"
          onClick={onBack}
        >
          <ArrowLeft />
          {t(($) => $.header.back)}
        </Button>
      )}
      <Button
        variant="ghost"
        size="sm"
        className="absolute top-16 right-12 text-muted-foreground hover:text-destructive"
        onClick={logout}
      >
        <LogOut />
        {t(($) => $.header.log_out)}
      </Button>
      <div className="flex flex-1 flex-col items-center justify-center px-6 pb-12">
        {children}
      </div>
    </div>
  );
}

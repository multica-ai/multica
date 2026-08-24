"use client";

import { useState } from "react";
import { UserPlus } from "lucide-react";
import { Avatar, AvatarFallback } from "@multica/ui/components/ui/avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { InviteDialog } from "./invite-dialog";
import type { InviteEligibility, PendingInvitation, WorkspaceMember } from "@/lib/types";

function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const first = words[0];
  if (!first) return "?";
  const last = words[words.length - 1];
  if (words.length === 1 || !last) return first.slice(0, 2).toUpperCase();
  return (first.charAt(0) + last.charAt(0)).toUpperCase();
}

export function inviteTooltipMessage(inviteEligibility: InviteEligibility): string {
  if (inviteEligibility.eligible) return "Invite a user to this workspace";
  if (inviteEligibility.reason === "pat-missing") {
    return "The admin dashboard's BOT_PAT isn't configured — invites are unavailable until this deployment is fixed.";
  }
  return `${inviteEligibility.botEmail} isn't an owner/admin of this workspace — ask an existing owner to add it before inviting through this dashboard.`;
}

interface MembersSectionProps {
  workspaceId: string;
  members: WorkspaceMember[];
  pendingInvitations: PendingInvitation[];
  inviteEligibility: InviteEligibility;
}

export function MembersSection({ workspaceId, members, pendingInvitations, inviteEligibility }: MembersSectionProps) {
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-label font-medium text-muted-foreground uppercase tracking-wide">Members</h3>
        <Tooltip>
          {/*
            focusableWhenDisabled makes Base UI render aria-disabled instead of
            the native `disabled` attribute (it still blocks the click). A
            native disabled button can't receive hover/focus, which would
            make this Tooltip unreachable in the ineligible state — see
            packages/views/projects/components/project-resources-section.tsx
            for the same pattern applied to a raw <button>.
          */}
          <TooltipTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5 aria-disabled:opacity-50 aria-disabled:cursor-not-allowed"
                disabled={!inviteEligibility.eligible}
                focusableWhenDisabled
                onClick={() => setDialogOpen(true)}
              >
                <UserPlus className="size-4" aria-hidden />
                Invite
              </Button>
            }
          />
          <TooltipContent>{inviteTooltipMessage(inviteEligibility)}</TooltipContent>
        </Tooltip>
      </div>
      {members.length === 0 ? (
        <p className="text-body text-muted-foreground">No members.</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {members.map((m) => (
            <li
              key={m.id}
              className="flex items-center gap-1.5 rounded-full bg-muted py-0.5 pl-0.5 pr-2 text-caption text-foreground"
            >
              <Avatar size="sm">
                <AvatarFallback>{initials(m.name)}</AvatarFallback>
              </Avatar>
              {m.name}
            </li>
          ))}
        </ul>
      )}
      {pendingInvitations.length > 0 && (
        <ul className="mt-1.5 flex flex-wrap gap-1.5">
          {pendingInvitations.map((invitation) => (
            <li
              key={invitation.email}
              className="flex items-center gap-1.5 rounded-full border border-dashed border-border py-0.5 px-2 text-caption text-muted-foreground"
            >
              {invitation.email} · pending
            </li>
          ))}
        </ul>
      )}
      <InviteDialog
        workspaceId={workspaceId}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        existingMemberEmails={members.map((m) => m.email.toLowerCase())}
        pendingInvitationEmails={pendingInvitations.map((invitation) => invitation.email.toLowerCase())}
      />
    </section>
  );
}

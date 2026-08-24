"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@multica/ui/components/ui/field";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useInviteMember } from "@/lib/hooks";

type Role = "admin" | "member";

const ROLE_ITEMS: Record<Role, string> = { member: "Member", admin: "Admin" };

interface InviteDialogProps {
  workspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existingMemberEmails: string[];
  pendingInvitationEmails: string[];
}

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// Client-side mirror of the route handler's LBYL pre-checks
// (lib/invite-eligibility.ts + the already-member/already-pending reads in
// app/api/workspaces/[id]/invitations/route.ts): validated against data the
// panel already loaded, so a doomed submit is caught before any network
// round trip. The route handler re-runs the same checks against a fresh
// read — this is a UX nicety, not the source of truth.
function clientSideError(
  email: string,
  existingMemberEmails: string[],
  pendingInvitationEmails: string[],
): string | null {
  const normalized = email.trim().toLowerCase();
  if (!normalized) return null;
  if (!EMAIL_PATTERN.test(normalized)) return "Enter a valid email address.";
  if (existingMemberEmails.includes(normalized)) return "This email is already a member of the workspace.";
  if (pendingInvitationEmails.includes(normalized)) return "This email already has a pending invitation.";
  return null;
}

export function InviteDialog({
  workspaceId,
  open,
  onOpenChange,
  existingMemberEmails,
  pendingInvitationEmails,
}: InviteDialogProps) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("member");
  const invite = useInviteMember(workspaceId);

  const error = clientSideError(email, existingMemberEmails, pendingInvitationEmails);
  const canSubmit = email.trim().length > 0 && error === null && !invite.isPending;

  function reset() {
    setEmail("");
    setRole("member");
    invite.reset();
  }

  function handleSubmit() {
    const trimmedEmail = email.trim();
    invite.mutate(
      { email: trimmedEmail.toLowerCase(), role },
      {
        onSuccess: () => {
          toast.success(`Invited ${trimmedEmail}`);
          reset();
          onOpenChange(false);
        },
        onError: (mutationError) => {
          toast.error(mutationError instanceof Error ? mutationError.message : "Failed to send invitation");
        },
      },
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Invite a member</DialogTitle>
        </DialogHeader>
        <Field data-invalid={error !== null}>
          <FieldLabel htmlFor="invite-email">Email</FieldLabel>
          <Input
            id="invite-email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="person@example.com"
            aria-invalid={error !== null}
          />
          <FieldError>{error}</FieldError>
        </Field>
        <Field>
          <FieldLabel htmlFor="invite-role">Role</FieldLabel>
          <Select items={ROLE_ITEMS} value={role} onValueChange={(value) => setRole(value ?? "member")}>
            <SelectTrigger id="invite-role">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">Member</SelectItem>
              <SelectItem value="admin">Admin</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {invite.isPending ? "Sending…" : "Send invite"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

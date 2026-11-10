"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Link2, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { copyText } from "@multica/ui/lib/clipboard";
import { api, errorCode } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { shareLinkListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { ShareLink, Workspace } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { seatInvitationCapacityFailure } from "../../settings/components/seat-invite-purchase";

/** Seven days, matching the members page's default share-link expiry. */
const SHARE_LINK_EXPIRES_IN_HOURS = 168;

export function parseInviteEmails(raw: string): { valid: string[]; invalid: string[] } {
  const tokens = raw
    .split(/[\s,;]+/)
    .map((token) => token.trim())
    .filter(Boolean);
  const unique = [...new Set(tokens.map((token) => token.toLowerCase()))];
  return {
    valid: unique.filter((email) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)),
    invalid: unique.filter((email) => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)),
  };
}

function usableShareLink(links: ShareLink[]): ShareLink | undefined {
  const now = Date.now();
  return links.find(
    (link) =>
      link.is_active &&
      link.role === "member" &&
      (!link.expires_at || new Date(link.expires_at).getTime() > now) &&
      (link.max_uses === null || link.use_count < link.max_uses),
  );
}

/**
 * The checklist's invite step. Invitations go out as members; anything past
 * that — admins, link expiry, seats — lives on the members page, which this
 * links to rather than repeating, so a seat limit hands over to the purchase
 * flow already there instead of growing a second one here.
 *
 * Someone who never named their workspace gets the chance now, because this
 * is the moment other people first see the name.
 */
export function InviteTeammatesDialog({
  workspace,
  offerRename,
  open,
  onOpenChange,
}: {
  workspace: Workspace;
  offerRename: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("onboarding");
  const qc = useQueryClient();
  const p = useWorkspacePaths();
  const { push, getShareableUrl } = useNavigation();
  const [name, setName] = useState(workspace.name);
  const [emails, setEmails] = useState("");
  const [sending, setSending] = useState(false);
  const [copying, setCopying] = useState(false);
  const [copied, setCopied] = useState(false);

  const parsed = parseInviteEmails(emails);
  const trimmedName = name.trim();
  const renamed = offerRename && trimmedName.length > 0 && trimmedName !== workspace.name;
  const canSend = !sending && (parsed.valid.length > 0 || renamed) && parsed.invalid.length === 0;

  const membersHref = `${p.settings()}?tab=members`;

  const rename = async () => {
    const updated = await api.updateWorkspace(workspace.id, { name: trimmedName });
    qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
      old?.map((ws) => (ws.id === updated.id ? updated : ws)),
    );
  };

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    try {
      if (renamed) await rename();
      const failed: string[] = [];
      let unsent: string[] = [];
      let sent = 0;
      for (const [i, email] of parsed.valid.entries()) {
        try {
          await api.createMember(workspace.id, { email, role: "member" });
          sent += 1;
        } catch (err) {
          const code = errorCode(err);
          if (seatInvitationCapacityFailure(code) || code === "seat_capacity_overcommitted") {
            unsent = parsed.valid.slice(i);
            break;
          }
          failed.push(email);
        }
      }
      if (sent > 0) {
        await qc.invalidateQueries({ queryKey: workspaceKeys.invitations(workspace.id) });
        toast.success(t(($) => $.invite.sent));
      }
      if (unsent.length > 0) {
        toast.error(t(($) => $.invite.seat_limit), {
          action: {
            label: t(($) => $.invite.open_members),
            onClick: () => push(membersHref),
          },
        });
      } else if (failed.length > 0) {
        toast.error(t(($) => $.invite.some_failed, { emails: failed.join(", ") }));
      }
      const retry = [...failed, ...unsent];
      setEmails(retry.join(", "));
      if (retry.length === 0) onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.invite.failed));
    } finally {
      setSending(false);
    }
  };

  const copyLink = async () => {
    setCopying(true);
    try {
      if (renamed) await rename();
      const links = await qc.fetchQuery({
        ...shareLinkListOptions(workspace.id),
        staleTime: 0,
      });
      const link =
        usableShareLink(links) ??
        (await api.createShareLink(workspace.id, {
          role: "member",
          expires_in: SHARE_LINK_EXPIRES_IN_HOURS,
        }));
      void qc.invalidateQueries({ queryKey: workspaceKeys.shareLinks(workspace.id) });
      const ok = await copyText(getShareableUrl(`/join?code=${link.code}`));
      if (!ok) {
        toast.error(t(($) => $.invite.copy_failed));
        return;
      }
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.invite.copy_failed));
    } finally {
      setCopying(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !sending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.invite.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.invite.description)}</DialogDescription>
        </DialogHeader>

        <div className="flex min-w-0 flex-col gap-4">
          {offerRename ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="invite-workspace-name">{t(($) => $.invite.workspace_name)}</Label>
              <Input
                id="invite-workspace-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={sending}
              />
              <p className="text-caption text-muted-foreground">
                {t(($) => $.invite.workspace_name_hint)}
              </p>
            </div>
          ) : null}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="invite-emails">{t(($) => $.invite.emails)}</Label>
            <Textarea
              id="invite-emails"
              autoFocus={!offerRename}
              value={emails}
              onChange={(e) => setEmails(e.target.value)}
              placeholder={t(($) => $.invite.emails_placeholder)}
              aria-invalid={parsed.invalid.length > 0 || undefined}
              disabled={sending}
              className="min-h-20"
            />
            {parsed.invalid.length > 0 ? (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.invite.invalid_emails, { emails: parsed.invalid.join(", ") })}
              </p>
            ) : null}
          </div>

          <div className="flex items-center justify-between gap-3 rounded-lg bg-muted/60 px-3 py-2.5">
            <div className="min-w-0">
              <div className="text-label font-medium text-foreground">
                {t(($) => $.invite.link_title)}
              </div>
              <div className="text-caption text-muted-foreground">
                {t(($) => $.invite.link_hint)}
              </div>
            </div>
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={copyLink}
              disabled={copying || sending}
              aria-busy={copying}
            >
              {copying ? (
                <Loader2 className="animate-spin" />
              ) : copied ? (
                <Check className="text-success" />
              ) : (
                <Link2 />
              )}
              {copied ? t(($) => $.invite.link_copied) : t(($) => $.invite.copy_link)}
            </Button>
          </div>
        </div>

        <DialogFooter className="sm:justify-between">
          <Button
            variant="ghost"
            className="text-muted-foreground"
            onClick={() => {
              onOpenChange(false);
              push(membersHref);
            }}
            disabled={sending}
          >
            {t(($) => $.invite.more_options)}
          </Button>
          <Button onClick={send} disabled={!canSend} aria-busy={sending}>
            {sending ? <Loader2 className="animate-spin" /> : null}
            {parsed.valid.length > 0 ? t(($) => $.invite.send) : t(($) => $.invite.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

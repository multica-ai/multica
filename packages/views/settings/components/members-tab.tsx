"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Clock,
  Copy,
  Crown,
  Link,
  Loader2,
  Mail,
  MoreHorizontal,
  Plus,
  Shield,
  Trash2,
  User,
  UserMinus,
  X,
} from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { useOptionalNavigation } from "../../navigation";
import type {
  Invitation,
  MemberRole,
  MemberWithUser,
  PurchaseWorkspaceSeatsRequest,
  ShareLink,
  WorkspaceSeatPurchasePreview,
} from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import {
  usePreviewWorkspaceSeatPurchase,
  usePurchaseWorkspaceSeats,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  invitationListOptions,
  memberListOptions,
  shareLinkListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { api, errorCode } from "@multica/core/api";
import { useLocale, useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";
import { formatStripeMinorAmount } from "./billing-format";
import {
  isSingleSeatInvitePreview,
  purchasedSeatIsReadyForInvitation,
  seatInvitationCanRetryAfterPurchase,
  seatInvitationCapacityFailure,
  seatPurchaseCanRetryWithSameQuote,
  seatPurchaseMatchesPreview,
} from "./seat-invite-purchase";

const SEAT_PURCHASE_CONFIRM_TIMEOUT_MS = 2 * 60_000;

type InviteSeatPurchase = {
  workspaceId: string;
  email: string;
  role: MemberRole;
  preview: WorkspaceSeatPurchasePreview;
  idempotencyKey: string;
  phase: "review" | "purchasing" | "waiting" | "inviting" | "error";
  submittedAt?: number;
  error?: string;
  retryable?: boolean;
};

function createSeatPurchaseKey(workspaceId: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `invite-seat-${workspaceId}-${suffix}`.slice(0, 200);
}

const ROLE_ICONS: Record<MemberRole, typeof Crown> = {
  owner: Crown,
  admin: Shield,
  member: User,
};

// Builds the shareable URL for a share-link invite. Prefers the navigation
// adapter's getShareableUrl (works on desktop where window.location.origin is
// not the public web origin), falling back to the browser origin on web.
function buildShareLinkUrl(
  navigation: ReturnType<typeof useOptionalNavigation>,
  code: string,
): string {
  const joinPath = `/join?code=${code}`;
  if (navigation?.getShareableUrl) {
    return navigation.getShareableUrl(joinPath);
  }
  return `${typeof window !== "undefined" ? window.location.origin : ""}${joinPath}`;
}

function useRoleLabels() {
  const { t } = useT("settings");
  return {
    owner: {
      label: t(($) => $.members.roles.owner.label),
      description: t(($) => $.members.roles.owner.description),
      icon: ROLE_ICONS.owner,
    },
    admin: {
      label: t(($) => $.members.roles.admin.label),
      description: t(($) => $.members.roles.admin.description),
      icon: ROLE_ICONS.admin,
    },
    member: {
      label: t(($) => $.members.roles.member.label),
      description: t(($) => $.members.roles.member.description),
      icon: ROLE_ICONS.member,
    },
  } as const;
}

function MemberRow({
  member,
  canManage,
  canManageOwners,
  ownerCount,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  /** Total number of owners in this workspace — needed to gate demoting the
   *  last owner per `workspace.go:497-507`. */
  ownerCount: number;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}) {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const isLastOwner = member.role === "owner" && ownerCount <= 1;
  const showMenu = canEditRole || canRemove;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size="lg" />
      <div className="min-w-0 flex-1">
        <div className="text-body font-medium truncate">{member.name}</div>
        <div className="text-caption text-muted-foreground truncate">{member.email}</div>
      </div>
      {showMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" disabled={busy}>
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-auto">
            {canEditRole && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Shield className="h-3.5 w-3.5" />
                  {t(($) => $.members.change_role)}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                    ([role, config]) => {
                      if (role === "owner" && !canManageOwners) return null;
                      const Icon = config.icon;
                      const wouldDemoteLastOwner =
                        isLastOwner && role !== "owner";
                      return (
                        <DropdownMenuItem
                          key={role}
                          onClick={() =>
                            wouldDemoteLastOwner ? undefined : onRoleChange(role)
                          }
                          disabled={wouldDemoteLastOwner}
                          title={
                            wouldDemoteLastOwner
                              ? t(($) => $.members.cannot_demote_last_owner_title)
                              : undefined
                          }
                        >
                          <Icon className="h-3.5 w-3.5" />
                          <div className="flex flex-col">
                            <span>{config.label}</span>
                            <span className="text-caption text-muted-foreground font-normal">
                              {wouldDemoteLastOwner
                                ? t(($) => $.members.cannot_demote_last_owner)
                                : config.description}
                            </span>
                          </div>
                          {member.role === role && (
                            <span className="ml-auto text-caption text-muted-foreground">{"✓"}</span>
                          )}
                        </DropdownMenuItem>
                      );
                    }
                  )}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {canEditRole && canRemove && <DropdownMenuSeparator />}
            {canRemove && (
              <DropdownMenuItem variant="destructive" onClick={onRemove}>
                <UserMinus className="h-3.5 w-3.5" />
                {t(($) => $.members.remove_action)}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      <Badge variant="secondary">
        <RoleIcon className="h-3 w-3" />
        {rc.label}
      </Badge>
    </div>
  );
}

function InvitationRow({
  invitation,
  canManage,
  onRevoke,
  busy,
}: {
  invitation: Invitation;
  canManage: boolean;
  onRevoke: () => void;
  busy: boolean;
}) {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const rc = roleConfig[invitation.role];

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <Mail className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-body font-medium truncate">{invitation.invitee_email}</div>
        <div className="flex items-center gap-1 text-caption text-muted-foreground">
          <Clock className="h-3 w-3" />
          <span>{t(($) => $.members.pending_status)}</span>
        </div>
      </div>
      {canManage && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onRevoke}
          title={t(($) => $.members.revoke_invitation_tooltip)}
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      <Badge variant="outline">
        {rc.label}
      </Badge>
    </div>
  );
}

function ShareLinkRow({
  link,
  onRevoke,
  busy,
  onCopy,
}: {
  link: ShareLink;
  onRevoke: () => void;
  busy: boolean;
  onCopy: () => void;
}) {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const rc = roleConfig[link.role];
  const navigation = useOptionalNavigation();
  const joinUrl = buildShareLinkUrl(navigation, link.code);

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <Link className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-1 text-body font-medium">
          <span>{t(($) => $.members.share_link_uses, { used: link.use_count, max: link.max_uses ?? "∞" })}</span>
          {link.expires_at && <span>· {t(($) => $.members.share_link_expires, { date: new Date(link.expires_at).toLocaleDateString() })}</span>}
        </div>
        <div
          className="truncate font-mono text-caption text-muted-foreground"
          title={joinUrl}
        >
          {joinUrl}
        </div>
      </div>
      <Button
        variant="ghost"
        size="icon-sm"
        onClick={onCopy}
        title={t(($) => $.members.share_link_copy_tooltip)}
      >
        <Copy className="h-4 w-4 text-muted-foreground" />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={busy}
        onClick={onRevoke}
        title={t(($) => $.members.share_link_revoke_tooltip)}
      >
        <Trash2 className="h-4 w-4 text-muted-foreground" />
      </Button>
      <Badge variant="outline">
        {rc.label}
      </Badge>
    </div>
  );
}

export function MembersTab() {
  const { t } = useT("settings");
  const { t: billingT } = useT("billing");
  const locale = useLocale();
  const roleConfig = useRoleLabels();
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const navigation = useOptionalNavigation();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: invitations = [] } = useQuery(invitationListOptions(wsId));

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [inviteSeatPurchase, setInviteSeatPurchase] =
    useState<InviteSeatPurchase | null>(null);
  const dispatchedInvitePurchaseKey = useRef<string | null>(null);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [invitationActionId, setInvitationActionId] = useState<string | null>(null);
  const [shareLinkActionId, setShareLinkActionId] = useState<string | null>(null);
  const [shareLinkLoading, setShareLinkLoading] = useState(false);
  const [shareLinkRole, setShareLinkRole] = useState<MemberRole>("member");
  const [shareLinkExpiry, setShareLinkExpiry] = useState<string>("168"); // default 7 days
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);
  const previewSeatPurchase = usePreviewWorkspaceSeatPurchase();
  const purchaseSeats = usePurchaseWorkspaceSeats(wsId);
  const seatPurchaseSummary = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    enabled:
      inviteSeatPurchase?.phase === "waiting" &&
      inviteSeatPurchase.workspaceId === wsId,
    staleTime: 0,
    refetchInterval:
      inviteSeatPurchase?.phase === "waiting" ? 2_000 : false,
  });

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";
  const ownerCount = members.filter((m) => m.role === "owner").length;
  // Only owners/admins may list share links; skip the request for plain
  // members (the server would 403) once the current member's role is known.
  const { data: shareLinks = [] } = useQuery(shareLinkListOptions(wsId, canManageWorkspace));

  const sendInvitation = useCallback(
    async (email: string, role: MemberRole) => {
      if (!workspace) return;
      await api.createMember(workspace.id, { email, role });
      setInviteEmail("");
      setInviteRole("member");
      await qc.invalidateQueries({
        queryKey: workspaceKeys.invitations(wsId),
      });
      toast.success(t(($) => $.members.toast_invitation_sent));
    },
    [qc, t, workspace, wsId],
  );

  const handleInviteMember = async () => {
    if (!workspace) return;
    const email = inviteEmail.trim();
    const role = inviteRole;
    setInviteLoading(true);
    try {
      await sendInvitation(email, role);
    } catch (e) {
      const code = errorCode(e);
      const capacityFailure = seatInvitationCapacityFailure(code);
      if (code === "seat_capacity_overcommitted") {
        try {
          const summary = await qc.fetchQuery({
            ...workspaceSubscriptionSummaryOptions(wsId),
            staleTime: 0,
          });
          const capacity = summary?.seatCapacity;
          toast.error(
            billingT(($) => $.workspace.seats.members_over_capacity_title),
            capacity
              ? {
                  description: billingT(
                    ($) => $.workspace.seats.occupancy_over_capacity_description,
                    {
                      // Cloud refuses on used + reserved, so pending
                      // invitations have to appear here: the most common
                      // trigger is a first ledger snapshot whose member count
                      // alone still fits inside the purchased seats.
                      occupied: capacity.used + capacity.reserved,
                      purchased: capacity.purchased,
                      members: capacity.used,
                      reserved: capacity.reserved,
                    },
                  ),
                }
              : undefined,
          );
        } catch {
          toast.error(
            billingT(($) => $.workspace.seats.members_over_capacity_title),
          );
        }
        return;
      }
      if (capacityFailure === "full") {
        try {
          const preview = await previewSeatPurchase.mutateAsync({
            additionalSeats: 1,
          });
          if (!isSingleSeatInvitePreview(preview)) {
            toast.error(
              billingT(($) => $.workspace.seat_purchase.preview_unreadable),
            );
            return;
          }
          setInviteSeatPurchase({
            workspaceId: workspace.id,
            email,
            role,
            preview,
            idempotencyKey: createSeatPurchaseKey(workspace.id),
            phase: "review",
          });
          dispatchedInvitePurchaseKey.current = null;
        } catch (previewError) {
          toast.error(
            errorCode(previewError) === "seat_purchase_in_progress"
              ? billingT(($) => $.workspace.seat_purchase.in_progress)
              : billingT(($) => $.workspace.seat_purchase.preview_failed),
          );
        }
        return;
      }
      if (capacityFailure === "unavailable") {
        toast.error(t(($) => $.members.toast_seat_capacity_unavailable));
        return;
      }
      if (capacityFailure === "rate_limited") {
        toast.error(t(($) => $.members.toast_seat_capacity_rate_limited));
        return;
      }
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invitation_failed));
    } finally {
      setInviteLoading(false);
    }
  };

  const handlePurchaseSeatAndInvite = async () => {
    if (
      !inviteSeatPurchase ||
      (inviteSeatPurchase.phase !== "review" &&
        !(inviteSeatPurchase.phase === "error" && inviteSeatPurchase.retryable))
    ) {
      return;
    }
    const current = inviteSeatPurchase;
    if (!workspace || current.workspaceId !== workspace.id) {
      setInviteSeatPurchase(null);
      return;
    }
    // A transient failure may happen after the purchase succeeded and the
    // first invitation dispatch ran. Reusing the same idempotency key is safe,
    // but the dispatch guard must be reopened for the retried invitation.
    dispatchedInvitePurchaseKey.current = null;
    setInviteSeatPurchase({
      ...current,
      phase: "purchasing",
      error: undefined,
      retryable: false,
    });
    const request: PurchaseWorkspaceSeatsRequest = {
      additionalSeats: current.preview.additionalSeats,
      expectedCurrentSeats: current.preview.currentSeats,
      expectedPurchaseVersion: current.preview.purchaseVersion,
      acceptedProrationAmount: current.preview.prorationAmount,
      currency: current.preview.currency,
      idempotencyKey: current.idempotencyKey,
    };
    try {
      const response = await purchaseSeats.mutateAsync(request);
      if (!seatPurchaseMatchesPreview(response, current.preview)) {
        setInviteSeatPurchase({
          ...current,
          phase: "error",
          error: billingT(
            ($) => $.workspace.seat_purchase.purchase_unreadable,
          ),
          retryable: true,
        });
        return;
      }
      const submittedAt = Date.now();
      setInviteSeatPurchase({
        ...current,
        phase: "waiting",
        submittedAt,
        error: undefined,
        retryable: false,
      });
      await qc.invalidateQueries({
        queryKey: workspaceSubscriptionSummaryOptions(wsId).queryKey,
      });
    } catch (error) {
      const code = errorCode(error);
      const message =
        code === "seat_purchase_payment_failed"
          ? billingT(($) => $.workspace.seat_purchase.payment_failed)
          : code === "seat_purchase_in_progress"
            ? billingT(($) => $.workspace.seat_purchase.in_progress)
            : code === "seat_quote_changed" || code === "seat_capacity_changed"
              ? billingT(($) => $.workspace.seat_purchase.quote_changed)
              : billingT(($) => $.workspace.seat_purchase.purchase_failed);
      setInviteSeatPurchase({
        ...current,
        phase: "error",
        error: message,
        retryable: seatPurchaseCanRetryWithSameQuote(code),
      });
    }
  };

  useEffect(() => {
    setInviteSeatPurchase((current) =>
      current && current.workspaceId !== wsId ? null : current,
    );
  }, [wsId]);

  useEffect(() => {
    const purchase = inviteSeatPurchase;
    if (
      purchase?.phase !== "waiting" ||
      purchase.workspaceId !== wsId ||
      purchase.submittedAt == null ||
      dispatchedInvitePurchaseKey.current === purchase.idempotencyKey ||
      !purchasedSeatIsReadyForInvitation(
        seatPurchaseSummary.data,
        purchase.preview,
        purchase.submittedAt,
        seatPurchaseSummary.dataUpdatedAt,
      )
    ) {
      return;
    }

    dispatchedInvitePurchaseKey.current = purchase.idempotencyKey;
    setInviteSeatPurchase({ ...purchase, phase: "inviting" });
    void sendInvitation(purchase.email, purchase.role)
      .then(() => setInviteSeatPurchase(null))
      .catch((error) => {
        const code = errorCode(error);
        const capacityFailure = seatInvitationCapacityFailure(code);
        let message: string;
        switch (capacityFailure) {
          case "full":
            message = t(($) => $.members.seat_purchase_capacity_taken);
            break;
          case "unavailable":
            message = t(($) => $.members.toast_seat_capacity_unavailable);
            break;
          case "rate_limited":
            message = t(($) => $.members.toast_seat_capacity_rate_limited);
            break;
          default:
            message =
              error instanceof Error
                ? error.message
                : t(($) => $.members.toast_invitation_failed);
        }
        setInviteSeatPurchase({
          ...purchase,
          phase: "error",
          retryable: seatInvitationCanRetryAfterPurchase(code),
          error: message,
        });
      });
  }, [
    inviteSeatPurchase,
    seatPurchaseSummary.data,
    seatPurchaseSummary.dataUpdatedAt,
    sendInvitation,
    t,
    wsId,
  ]);

  useEffect(() => {
    if (
      inviteSeatPurchase?.phase !== "waiting" ||
      inviteSeatPurchase.submittedAt == null
    ) {
      return;
    }
    const elapsed = Date.now() - inviteSeatPurchase.submittedAt;
    const timeout = window.setTimeout(() => {
      setInviteSeatPurchase((current) =>
        current?.phase === "waiting"
          ? {
              ...current,
              phase: "error",
              error: t(($) => $.members.seat_purchase_timeout),
              retryable: false,
            }
          : current,
      );
    }, Math.max(0, SEAT_PURCHASE_CONFIRM_TIMEOUT_MS - elapsed));
    return () => window.clearTimeout(timeout);
  }, [inviteSeatPurchase?.phase, inviteSeatPurchase?.submittedAt, t]);

  const handleRevokeInvitation = (invitation: Invitation) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.revoke_invitation_title),
      description: t(($) => $.members.revoke_invitation_description, { email: invitation.invitee_email }),
      variant: "destructive",
      onConfirm: async () => {
        setInvitationActionId(invitation.id);
        try {
          await api.revokeInvitation(workspace.id, invitation.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
          toast.success(t(($) => $.members.toast_invitation_revoked));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invitation_revoke_failed));
        } finally {
          setInvitationActionId(null);
        }
      },
    });
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await api.updateMember(workspace.id, memberId, { role });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(t(($) => $.members.toast_role_updated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_role_failed));
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = (member: MemberWithUser) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.remove_member_title, { name: member.name }),
      description: t(($) => $.members.remove_member_description, { name: member.name, workspace: workspace.name }),
      variant: "destructive",
      onConfirm: async () => {
        setMemberActionId(member.id);
        try {
          await api.deleteMember(workspace.id, member.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
          toast.success(t(($) => $.members.toast_member_removed));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_member_remove_failed));
        } finally {
          setMemberActionId(null);
        }
      },
    });
  };

  const handleCreateShareLink = async () => {
    if (!workspace) return;
    setShareLinkLoading(true);
    try {
      await api.createShareLink(workspace.id, { role: shareLinkRole, expires_in: parseInt(shareLinkExpiry) || undefined });
      qc.invalidateQueries({ queryKey: workspaceKeys.shareLinks(wsId) });
      toast.success(t(($) => $.members.toast_share_link_created));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_share_link_failed));
    } finally {
      setShareLinkLoading(false);
    }
  };

  const handleRevokeShareLink = (link: ShareLink) => {
    if (!workspace) return;
    setShareLinkActionId(link.id);
    api.revokeShareLink(workspace.id, link.id)
      .then(() => {
        qc.invalidateQueries({ queryKey: workspaceKeys.shareLinks(wsId) });
        toast.success(t(($) => $.members.toast_share_link_revoked));
      })
      .catch((e) => {
        toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_share_link_revoke_failed));
      })
      .finally(() => setShareLinkActionId(null));
  };

  const handleCopyShareLink = (link: ShareLink) => {
    const joinUrl = buildShareLinkUrl(navigation, link.code);
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(joinUrl).then(
        () => toast.success(t(($) => $.members.toast_share_link_copied)),
        () => toast.error(t(($) => $.members.toast_share_link_copy_failed)),
      );
    } else {
      const textArea = document.createElement("textarea");
      textArea.value = joinUrl;
      textArea.style.position = "fixed";
      textArea.style.left = "-9999px";
      document.body.appendChild(textArea);
      textArea.select();
      try {
        document.execCommand("copy");
        toast.success(t(($) => $.members.toast_share_link_copied));
      } catch {
        toast.error(t(($) => $.members.toast_share_link_copy_failed));
      }
      document.body.removeChild(textArea);
    }
  };

  if (!workspace) return null;

  return (
    <SettingsTab title={t(($) => $.page.tabs.members)}>
      <SettingsSection title={t(($) => $.members.section_title, { count: members.length })}>

        {canManageWorkspace && (
          <Card>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-body font-medium">{t(($) => $.members.invite_title)}</h3>
              </div>
              <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                <Input
                  type="email"
                  name="invite-email"
                  autoComplete="email"
                  spellCheck={false}
                  aria-label={t(($) => $.members.invite_email_placeholder)}
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder={t(($) => $.members.invite_email_placeholder)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && inviteEmail.trim()) handleInviteMember();
                  }}
                />
                <Select
                  items={(["member", "admin"] as const).map((value) => ({
                    value,
                    label: roleConfig[value].label,
                  }))}
                  value={inviteRole}
                  onValueChange={(value) => setInviteRole(value as MemberRole)}
                >
                  <SelectTrigger size="sm">
                    <SelectValue>{() => roleConfig[inviteRole].label}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">{roleConfig.member.label}</SelectItem>
                    <SelectItem value="admin">{roleConfig.admin.label}</SelectItem>
                  </SelectContent>
                </Select>
                <Button
                  onClick={handleInviteMember}
                  disabled={inviteLoading || !inviteEmail.trim()}
                >
                  {inviteLoading ? t(($) => $.members.inviting) : t(($) => $.members.invite_button)}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <SettingsCard>
            {members.map((m) => (
              <div key={m.id}>
                <MemberRow
                  member={m}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  ownerCount={ownerCount}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                />
              </div>
            ))}
          </SettingsCard>
        ) : (
          <p className="text-body text-muted-foreground">{t(($) => $.members.no_members)}</p>
        )}
      </SettingsSection>

      {invitations.length > 0 && (
        <SettingsSection title={t(($) => $.members.pending_title, { count: invitations.length })}>
          <SettingsCard>
            {invitations.map((inv) => (
              <div key={inv.id}>
                <InvitationRow
                  invitation={inv}
                  canManage={canManageWorkspace}
                  onRevoke={() => handleRevokeInvitation(inv)}
                  busy={invitationActionId === inv.id}
                />
              </div>
            ))}
          </SettingsCard>
        </SettingsSection>
      )}

      {canManageWorkspace && (
        <SettingsSection title={t(($) => $.members.share_links_title, { count: shareLinks.length })}>
          <Card>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Link className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-body font-medium">{t(($) => $.members.share_links_create_title)}</h3>
              </div>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <div className="flex min-w-0 flex-1 basis-40 items-center gap-2">
                  <span className="text-body text-muted-foreground shrink-0">{t(($) => $.members.role_field)}</span>
                  <Select
                    items={(["member", "admin"] as const).map((value) => ({
                      value,
                      label: roleConfig[value].label,
                    }))}
                    value={shareLinkRole}
                    onValueChange={(value) => setShareLinkRole(value as MemberRole)}
                  >
                    <SelectTrigger size="sm">
                      <SelectValue>{() => roleConfig[shareLinkRole].label}</SelectValue>
                    </SelectTrigger>
                    <SelectContent className="min-w-0">
                      <SelectItem value="member">{roleConfig.member.label}</SelectItem>
                      <SelectItem value="admin">{roleConfig.admin.label}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex min-w-0 flex-1 basis-40 items-center gap-2">
                  <span className="text-body text-muted-foreground shrink-0">{t(($) => $.members.expiry_field)}</span>
                  <Select
                    items={[
                      { value: "24", label: t(($) => $.members.expiry_24h) },
                      { value: "168", label: t(($) => $.members.expiry_7d) },
                      { value: "720", label: t(($) => $.members.expiry_30d) },
                      { value: "0", label: t(($) => $.members.expiry_never) },
                    ]}
                    value={shareLinkExpiry}
                    onValueChange={(v) => v && setShareLinkExpiry(v)}
                  >
                    <SelectTrigger size="sm">
                      <SelectValue>{() => {
                        const opts: Record<string, string> = {
                          "24": t(($) => $.members.expiry_24h),
                          "168": t(($) => $.members.expiry_7d),
                          "720": t(($) => $.members.expiry_30d),
                          "0": t(($) => $.members.expiry_never),
                        };
                        return opts[shareLinkExpiry] || shareLinkExpiry;
                      }}</SelectValue>
                    </SelectTrigger>
                    <SelectContent className="min-w-0">
                      <SelectItem value="24">{t(($) => $.members.expiry_24h)}</SelectItem>
                      <SelectItem value="168">{t(($) => $.members.expiry_7d)}</SelectItem>
                      <SelectItem value="720">{t(($) => $.members.expiry_30d)}</SelectItem>
                      <SelectItem value="0">{t(($) => $.members.expiry_never)}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <Button onClick={handleCreateShareLink} disabled={shareLinkLoading} className="shrink-0">
                  {shareLinkLoading ? t(($) => $.members.share_links_creating) : t(($) => $.members.share_links_create_button)}
                </Button>
              </div>
            </CardContent>
          </Card>
          {shareLinks.length > 0 && (
            <SettingsCard>
              {shareLinks.map((link) => (
                <div key={link.id}>
                  <ShareLinkRow
                    link={link}
                    onRevoke={() => handleRevokeShareLink(link)}
                    busy={shareLinkActionId === link.id}
                    onCopy={() => handleCopyShareLink(link)}
                  />
                </div>
              ))}
            </SettingsCard>
          )}
        </SettingsSection>
      )}

      <AlertDialog
        open={inviteSeatPurchase !== null}
        onOpenChange={(open) => {
          if (
            !open &&
            (inviteSeatPurchase?.phase === "review" ||
              inviteSeatPurchase?.phase === "error")
          ) {
            setInviteSeatPurchase(null);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.members.seat_purchase_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {inviteSeatPurchase?.phase === "review"
                ? t(($) => $.members.seat_purchase_description, {
                    email: inviteSeatPurchase.email,
                  })
                : inviteSeatPurchase?.phase === "error"
                  ? inviteSeatPurchase.error
                  : t(($) => $.members.seat_purchase_waiting)}
            </AlertDialogDescription>
          </AlertDialogHeader>

          {inviteSeatPurchase?.phase === "review" && (
            <div className="divide-y rounded-lg border text-body">
              <div className="flex items-center justify-between gap-4 px-4 py-3">
                <span className="text-muted-foreground">
                  {billingT(($) => $.workspace.seat_purchase.seats_after)}
                </span>
                <span className="font-medium">
                  {billingT(($) => $.workspace.seats.seat_count, {
                    count: inviteSeatPurchase.preview.resultingSeats,
                  })}
                </span>
              </div>
              <div className="flex items-center justify-between gap-4 px-4 py-3">
                <span className="text-muted-foreground">
                  {billingT(($) => $.workspace.seat_purchase.charge_today)}
                </span>
                <span className="font-medium">
                  {formatStripeMinorAmount(
                    inviteSeatPurchase.preview.prorationAmount,
                    inviteSeatPurchase.preview.currency,
                    locale,
                  ) ?? "—"}
                </span>
              </div>
              <div className="flex items-center justify-between gap-4 px-4 py-3">
                <span className="text-muted-foreground">
                  {billingT(($) => $.workspace.seat_purchase.next_invoice)}
                </span>
                <span className="font-medium">
                  {formatStripeMinorAmount(
                    inviteSeatPurchase.preview.nextInvoiceAmount,
                    inviteSeatPurchase.preview.currency,
                    locale,
                  ) ?? "—"}
                </span>
              </div>
              <p className="px-4 py-3 text-caption text-muted-foreground">
                {billingT(($) => $.workspace.seat_purchase.tax_notice)}
              </p>
            </div>
          )}

          {(inviteSeatPurchase?.phase === "purchasing" ||
            inviteSeatPurchase?.phase === "waiting" ||
            inviteSeatPurchase?.phase === "inviting") && (
            <div className="flex items-center gap-2 text-body text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              {inviteSeatPurchase.phase === "inviting"
                ? t(($) => $.members.inviting)
                : t(($) => $.members.seat_purchase_waiting)}
            </div>
          )}

          <AlertDialogFooter>
            {(inviteSeatPurchase?.phase === "review" ||
              inviteSeatPurchase?.phase === "error") && (
              <AlertDialogCancel>
                {t(($) => $.members.confirm_cancel)}
              </AlertDialogCancel>
            )}
            {inviteSeatPurchase?.phase === "review" && (
              <AlertDialogAction
                onClick={(event) => {
                  event.preventDefault();
                  void handlePurchaseSeatAndInvite();
                }}
              >
                {t(($) => $.members.purchase_seat_and_invite)}
              </AlertDialogAction>
            )}
            {inviteSeatPurchase?.phase === "error" &&
              inviteSeatPurchase.retryable && (
                <AlertDialogAction
                  onClick={(event) => {
                    event.preventDefault();
                    void handlePurchaseSeatAndInvite();
                  }}
                >
                  {t(($) => $.members.retry_seat_purchase)}
                </AlertDialogAction>
              )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.members.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              {t(($) => $.members.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

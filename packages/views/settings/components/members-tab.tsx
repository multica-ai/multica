"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Check,
  Copy,
  Crown,
  Eye,
  FolderOpen,
  Link,
  Loader2,
  Mail,
  MoreHorizontal,
  Plus,
  Search,
  Shield,
  Trash2,
  User,
  UserMinus,
  Users,
  X,
} from "@multica/ui/icons";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useOptionalNavigation } from "../../navigation";
import type {
  Invitation,
  MemberRole,
  MemberWithUser,
  PurchaseWorkspaceSeatsRequest,
  ShareLink,
  WorkspaceSeatPurchasePreview,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
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
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import {
  clearUpgradeInvitesFor,
  isMemberLimitFull,
  isMemberLimitReachedError,
  memberLimitKeys,
  memberLimitOptions,
  readUpgradeInvite,
  saveUpgradeInvite,
  type UpgradeInvite,
  usePreviewWorkspaceSeatPurchase,
  refreshFeaturesOnLockedError,
  usePaidFeatureAccess,
  usePurchaseWorkspaceSeats,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureEnabled } from "@multica/core/config";
import {
  BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
  WORKSPACE_GUESTS_FLAG,
} from "@multica/core/feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  invitationListOptions,
  memberListOptions,
  shareLinkListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { api, errorCode } from "@multica/core/api";
import { useLocale, useT } from "../../i18n";
import { ObjectSectionEmpty } from "../../layout/object-page";
import {
  SettingsCard,
  SettingsReadOnlyNotice,
  SettingsTab,
  SettingsViewTabs,
} from "./settings-layout";
import { resolveSettingsLocation, settingsHref } from "./settings-navigation";
import { formatStripeMinorAmount } from "./billing-format";
import { PaidFeatureNotice, usePaidFeatureErrorMessage } from "../../billing";
import { GuestProjectsDialog, GuestProjectsField, useShareableProjects } from "./guest-projects";
import {
  isSingleSeatInvitePreview,
  purchasedSeatIsReadyForInvitation,
  seatInvitationCanRetryAfterPurchase,
  seatInvitationCapacityFailure,
  seatPurchaseCanRetryWithSameQuote,
  seatPurchaseMatchesPreview,
} from "./seat-invite-purchase";

const SEAT_PURCHASE_CONFIRM_TIMEOUT_MS = 2 * 60_000;

// Sending a held invitation right after its upgrade retries a member-limit
// refusal this many times, this far apart: past the server's five-second
// bound on re-reading Cloud's policy.
const HELD_INVITE_RETRIES = 2;
const HELD_INVITE_RETRY_MS = 6_000;

// Catches typos before an upgrade buys a seat for the invitation.
const INVITE_EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// "members" lists everyone; with guests on, "seated" and "guests" split it.
type MembersView = "members" | "seated" | "guests" | "invitations" | "links";

// A seat bought to invite someone, or to turn a guest into a member.
type InviteSeatPurchase = {
  workspaceId: string;
  email: string;
  role: MemberRole;
  projectIds?: string[];
  promote?: { memberId: string; name: string };
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
  guest: Eye,
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
    guest: {
      label: t(($) => $.members.roles.guest.label),
      description: t(($) => $.members.roles.guest.description),
      icon: ROLE_ICONS.guest,
    },
  } as const;
}

function MemberRow({
  member,
  canManage,
  canManageOwners,
  guestsEnabled,
  projectNames,
  ownerCount,
  isSelf,
  busy,
  onRoleChange,
  onManageProjects,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  /** Whether a member can be turned into a guest. */
  guestsEnabled: boolean;
  /** A guest's projects, shown beside its name. */
  projectNames: string[];
  /** Total number of owners in this workspace — needed to gate demoting the
   *  last owner per `workspace.go:497-507`. */
  ownerCount: number;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onManageProjects: () => void;
  onRemove: () => void;
}) {
  const { t } = useT("settings");
  const locale = useLocale();
  const roleConfig = useRoleLabels();
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const isLastOwner = member.role === "owner" && ownerCount <= 1;
  const isGuest = member.role === "guest";
  const showMenu = canEditRole || canRemove;
  // A guest becomes a member or admin first; ownership is not handed over
  // in the same step.
  const roleOptions = (Object.keys(roleConfig) as MemberRole[]).filter(
    (role) =>
      (role !== "owner" || (canManageOwners && !isGuest)) &&
      (role !== "guest" || guestsEnabled || isGuest),
  );

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size="lg" />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{member.name}</span>
          {isSelf ? (
            <Badge variant="secondary" className="shrink-0">
              {t(($) => $.members.you)}
            </Badge>
          ) : null}
          {isGuest && projectNames.length > 0 ? (
            <span className="truncate text-caption text-muted-foreground">
              {projectNames.join(t(($) => $.members.list_separator))}
            </span>
          ) : null}
        </div>
        <div className="truncate text-caption text-muted-foreground">{member.email}</div>
      </div>
      <span className="hidden w-32 shrink-0 text-caption tabular-nums text-muted-foreground md:block">
        {t(($) => $.members.joined, {
          date: new Date(member.created_at).toLocaleDateString(locale),
        })}
      </span>
      <span className="flex w-24 shrink-0 items-center gap-1.5 text-body">
        <RoleIcon className="size-3.5 text-muted-foreground" aria-hidden="true" />
        {rc.label}
      </span>
      <span className="flex size-7 shrink-0 items-center justify-center">
      {showMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <IconButton
                variant="ghost"
                size="icon-sm"
                disabled={busy}
                label={t(($) => $.members.member_actions, { name: member.name })}
              >
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </IconButton>
            }
          />
          <DropdownMenuContent align="end" className="w-60">
            {canEditRole && (
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.members.change_role)}</DropdownMenuLabel>
                {roleOptions.map((role) => {
                  const Icon = roleConfig[role].icon;
                  const current = member.role === role;
                  const wouldDemoteLastOwner = isLastOwner && role !== "owner";
                  // Moving across the guest line takes or frees a seat.
                  const seatHint =
                    isGuest && role === "member"
                      ? t(($) => $.members.takes_seat)
                      : !isGuest && role === "guest"
                        ? t(($) => $.members.frees_seat)
                        : null;
                  return (
                    <DropdownMenuItem
                      key={role}
                      onClick={() => {
                        if (!current && !wouldDemoteLastOwner) onRoleChange(role);
                      }}
                      disabled={wouldDemoteLastOwner}
                      title={
                        wouldDemoteLastOwner
                          ? t(($) => $.members.cannot_demote_last_owner_title)
                          : roleConfig[role].description
                      }
                    >
                      {current ? <Check className="h-3.5 w-3.5" /> : <Icon className="h-3.5 w-3.5" />}
                      <span>
                        {isGuest && role === "member"
                          ? t(($) => $.members.make_member)
                          : roleConfig[role].label}
                      </span>
                      {seatHint ? (
                        <span className="ml-auto pl-3 text-caption text-muted-foreground">{seatHint}</span>
                      ) : null}
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            )}
            {canEditRole && isGuest && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={onManageProjects}>
                  <FolderOpen className="h-3.5 w-3.5" />
                  {t(($) => $.members.manage_projects)}
                </DropdownMenuItem>
              </>
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
      </span>
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
  const locale = useLocale();
  const roleConfig = useRoleLabels();
  const rc = roleConfig[invitation.role];

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted">
        <Mail className="size-4 text-muted-foreground" aria-hidden="true" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium">{invitation.invitee_email}</div>
        <div className="truncate text-caption text-muted-foreground">
          {t(($) => $.members.invitation_meta, {
            sent: new Date(invitation.created_at).toLocaleDateString(locale),
            expires: new Date(invitation.expires_at).toLocaleDateString(locale),
          })}
        </div>
      </div>
      <span className="w-24 shrink-0 text-body">{rc.label}</span>
      <span className="flex size-7 shrink-0 items-center justify-center">
        {canManage && (
          <IconButton
            variant="ghost"
            size="icon-sm"
            disabled={busy}
            onClick={onRevoke}
            label={t(($) => $.members.revoke_invitation_aria, {
              email: invitation.invitee_email,
            })}
          >
            <X className="size-4 text-muted-foreground" />
          </IconButton>
        )}
      </span>
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
  const locale = useLocale();
  const roleConfig = useRoleLabels();
  const rc = roleConfig[link.role];
  const navigation = useOptionalNavigation();
  const joinUrl = buildShareLinkUrl(navigation, link.code);

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted">
        <Link className="size-4 text-muted-foreground" aria-hidden="true" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-1 text-body font-medium">
          <span>{t(($) => $.members.share_link_uses, { used: link.use_count, max: link.max_uses ?? "∞" })}</span>
          {link.expires_at && <span>· {t(($) => $.members.share_link_expires, { date: new Date(link.expires_at).toLocaleDateString(locale) })}</span>}
        </div>
        <TruncatedTooltip content={joinUrl} focusable>
          <div
            className="truncate font-mono text-caption text-muted-foreground"
          >
            {joinUrl}
          </div>
        </TruncatedTooltip>
      </div>
      <span className="w-24 shrink-0 text-body">{rc.label}</span>
      <IconButton
        variant="ghost"
        size="icon-sm"
        onClick={onCopy}
        label={t(($) => $.members.share_link_copy_tooltip)}
      >
        <Copy className="size-4 text-muted-foreground" />
      </IconButton>
      <IconButton
        variant="ghost"
        size="icon-sm"
        disabled={busy}
        onClick={onRevoke}
        label={t(($) => $.members.share_link_revoke_tooltip)}
      >
        <Trash2 className="size-4 text-muted-foreground" />
      </IconButton>
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
  const memberListQuery = useQuery(memberListOptions(wsId));
  const invitationListQuery = useQuery(invitationListOptions(wsId));
  const members = memberListQuery.data ?? [];
  const invitations = invitationListQuery.data ?? [];
  // An upgrade buys a seat for the invitation, so it waits until both lists
  // have loaded and the invitation is known to be sendable.
  const inviteChecksReady = memberListQuery.isSuccess && invitationListQuery.isSuccess;

  const [inviteOpen, setInviteOpen] = useState(false);
  const [shareLinkOpen, setShareLinkOpen] = useState(false);
  const requestedView = resolveSettingsLocation(
    navigation?.searchParams ?? new URLSearchParams(),
  ).section;
  const urlView: MembersView =
    requestedView === "invitations" ||
    requestedView === "links" ||
    requestedView === "seated" ||
    requestedView === "guests"
      ? requestedView
      : "members";
  const [localView, setLocalView] = useState<MembersView>(urlView);
  // With a router the URL owns the open list, so search results that land on
  // this page and back/forward switch it too. Without one it is local state.
  const view = navigation ? urlView : localView;
  const setView = (next: MembersView) => {
    if (!navigation) {
      setLocalView(next);
      return;
    }
    navigation.replace(
      settingsHref(navigation.pathname, navigation.searchParams, "members", {
        section: next === "members" ? undefined : next,
      }),
    );
  };
  const [memberQuery, setMemberQuery] = useState("");
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteProjects, setInviteProjects] = useState<string[]>([]);
  // Turning a member into a guest, or changing a guest's projects.
  const [projectsDialog, setProjectsDialog] = useState<{
    member: MemberWithUser;
    mode: "demote" | "edit";
  } | null>(null);
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
    actionLabel?: string;
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

  // The Team panel's invite button links here with `?invite=1`; it waits for
  // the member list so only managers get the dialog. Billing links back with
  // `?invite=resume&held_invite=<token>` once the upgrade started for that
  // held invitation has gone through, and the invitation is sent.
  const inviteRequest = navigation?.searchParams.get("invite") ?? null;
  const heldInviteToken = navigation?.searchParams.get("held_invite") ?? null;
  useEffect(() => {
    if (inviteRequest === null || !navigation || !currentMember || !workspace) return;
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("invite");
    params.delete("held_invite");
    navigation.replace(`${navigation.pathname}?${params}`);
    if (!canManageWorkspace) return;
    const held: UpgradeInvite | null =
      inviteRequest === "resume" && heldInviteToken
        ? readUpgradeInvite(workspace.slug, heldInviteToken)
        : null;
    setInviteOpen(true);
    if (!held) return;
    setInviteEmail(held.email);
    setInviteRole(held.role);
    // The held invitation is consumed only once it has been sent; if sending
    // still fails it stays prefilled here and held for another try.
    void qc
      .invalidateQueries({ queryKey: memberLimitKeys.all(wsId) })
      .then(() => inviteMember(held.email, held.role, { retryLimitRefusals: HELD_INVITE_RETRIES }));
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume the intent once the role is known
  }, [inviteRequest, heldInviteToken, currentMember?.role, workspace?.slug]);
  const isOwner = currentMember?.role === "owner";
  const ownerCount = members.filter((m) => m.role === "owner").length;
  // Only owners/admins may list share links; skip the request for plain
  // members (the server would 403) once the current member's role is known.
  const { data: shareLinks = [] } = useQuery(shareLinkListOptions(wsId, canManageWorkspace));
  const billingEnabled = useFeatureEnabled(BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG, false);
  const subscriptionSummary = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    enabled: billingEnabled && canManageWorkspace,
  }).data;
  const seats = subscriptionSummary?.seatCapacity;
  const memberLimit = useQuery({
    ...memberLimitOptions(wsId),
    enabled: canManageWorkspace,
  }).data;
  // Only an enforced limit is shown; an observed one admits everyone.
  const enforcedMemberLimit =
    memberLimit?.action === "enforce" && memberLimit.limit !== null
      ? {
          used: (memberLimit.members ?? 0) + (memberLimit.pendingInvitations ?? 0),
          limit: memberLimit.limit,
          pending: memberLimit.pendingInvitations ?? 0,
        }
      : null;
  const inviteBlockedByMemberLimit = isMemberLimitFull(memberLimit);
  // Upgrading needs Billing; without it a full workspace can only free a slot.
  // A lapsed subscription (past due beyond grace) cannot start a new
  // checkout: it is restored in Billing, not upgraded.
  const subscriptionNeedsAttention =
    inviteBlockedByMemberLimit &&
    billingEnabled &&
    subscriptionSummary?.availableActions.checkout === false;
  const canUpgradeForInvite =
    inviteBlockedByMemberLimit && billingEnabled && !subscriptionNeedsAttention;
  const guestsEnabled = useFeatureEnabled(WORKSPACE_GUESTS_FLAG, false);
  // Guests are a paid feature: a plan without them keeps its existing guests
  // (they can be removed, narrowed or made members) but adds no new ones.
  const guestAccess = usePaidFeatureAccess(wsId, "guests", { hasExistingConfig: false });
  const canAddGuests = guestsEnabled && guestAccess.canAdd;
  const paidFeatureError = usePaidFeatureErrorMessage();
  const shareableProjects = useShareableProjects(wsId);
  const projectTitles = new Map(shareableProjects.map((project) => [project.id, project.title]));
  const guestCount = members.filter((m) => m.role === "guest").length;
  // Workspaces that already have guests keep seeing them where adding guests
  // is not offered (Cloud before billing launches).
  const showGuestViews = guestsEnabled || guestCount > 0;
  // A guest takes no member slot, so a full workspace still invites guests.
  const inviteLimited = inviteRole !== "guest";
  const inviteFull = inviteLimited && inviteBlockedByMemberLimit;
  const inviteNeedsAttention = inviteLimited && subscriptionNeedsAttention;
  const inviteCanUpgrade = inviteLimited && canUpgradeForInvite;
  const guestInviteMissingProjects = inviteRole === "guest" && inviteProjects.length === 0;
  const needle = memberQuery.trim().toLocaleLowerCase();
  const peopleView = showGuestViews && (view === "seated" || view === "guests") ? view : "members";
  const visibleMembers = members.filter(
    (m) =>
      (peopleView !== "seated" || m.role !== "guest") &&
      (peopleView !== "guests" || m.role === "guest") &&
      (!needle ||
        m.name.toLocaleLowerCase().includes(needle) ||
        m.email.toLocaleLowerCase().includes(needle)),
  );

  const sendInvitation = useCallback(
    async (email: string, role: MemberRole, projectIds?: string[]) => {
      if (!workspace) return;
      await api.createMember(
        workspace.id,
        role === "guest" ? { email, role, project_ids: projectIds } : { email, role },
      );
      // The invitation is out, however it was sent (directly, after an
      // upgrade, or after buying a seat): nothing held for it still waits.
      clearUpgradeInvitesFor(workspace.slug, email);
      setInviteEmail("");
      setInviteRole("member");
      setInviteProjects([]);
      await qc.invalidateQueries({
        queryKey: workspaceKeys.invitations(wsId),
      });
      toast.success(t(($) => $.members.toast_invitation_sent));
    },
    [qc, t, workspace, wsId],
  );

  const changeRole = useCallback(
    async (memberId: string, role: MemberRole, projectIds?: string[]) => {
      if (!workspace) return;
      await api.updateMember(
        workspace.id,
        memberId,
        role === "guest" ? { role, project_ids: projectIds } : { role },
      );
      await qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      // Crossing the guest line takes or frees a member slot.
      void qc.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
      toast.success(t(($) => $.members.toast_role_updated));
    },
    [qc, t, workspace, wsId],
  );

  /**
   * Handles a refusal for lack of a seat, from an invitation or from turning a
   * guest into a member: offers to buy one when the workspace is full.
   * Returns false when the error is not about seats.
   */
  const handleSeatRefusal = async (
    e: unknown,
    request: Pick<InviteSeatPurchase, "email" | "role" | "projectIds" | "promote">,
  ): Promise<boolean> => {
    if (!workspace) return true;
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
      return true;
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
          return true;
        }
        // Hand over to the purchase confirmation rather than stacking a
        // second modal on the invite dialog.
        setInviteOpen(false);
        setInviteSeatPurchase({
          workspaceId: workspace.id,
          ...request,
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
      return true;
    }
    if (capacityFailure === "unavailable") {
      toast.error(t(($) => $.members.toast_seat_capacity_unavailable));
      return true;
    }
    if (capacityFailure === "rate_limited") {
      toast.error(t(($) => $.members.toast_seat_capacity_rate_limited));
      return true;
    }
    return false;
  };

  // Resolves true once the invitation is sent. retryLimitRefusals retries a
  // member-limit refusal after HELD_INVITE_RETRY_MS: right after an upgrade
  // the server may still hold the Free policy it re-read moments before.
  const inviteMember = async (
    email: string,
    role: MemberRole,
    {
      projectIds,
      retryLimitRefusals = 0,
    }: { projectIds?: string[]; retryLimitRefusals?: number } = {},
  ): Promise<boolean> => {
    if (!workspace) return false;
    setInviteLoading(true);
    try {
      for (let attempt = 0; ; attempt++) {
        try {
          await sendInvitation(email, role, projectIds);
          break;
        } catch (retryable) {
          if (!isMemberLimitReachedError(retryable) || attempt >= retryLimitRefusals) throw retryable;
          await new Promise((resolve) => setTimeout(resolve, HELD_INVITE_RETRY_MS));
        }
      }
      setInviteOpen(false);
      void qc.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
      return true;
    } catch (e) {
      if (isMemberLimitReachedError(e)) {
        // The limit filled up after this dialog last read it. Refetch so the
        // dialog switches to its full state and offers the upgrade.
        await qc.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
        toast.error(t(($) => $.members.toast_member_limit_reached));
        return false;
      }
      if (await handleSeatRefusal(e, { email, role, projectIds })) return false;
      refreshFeaturesOnLockedError(qc, wsId, e);
      toast.error(
        paidFeatureError(e, e instanceof Error ? e.message : t(($) => $.members.toast_invitation_failed)),
      );
      return false;
    } finally {
      setInviteLoading(false);
    }
  };

  const handleInviteMember = () =>
    inviteMember(inviteEmail.trim(), inviteRole, {
      projectIds: inviteRole === "guest" ? inviteProjects : undefined,
    });

  // Why an invitation could not be sent even after paying, checked before
  // the upgrade buys a seat for it. The server re-checks on send.
  const upgradeInviteProblem = (email: string): string | null => {
    if (!inviteChecksReady) {
      return t(($) => $.members.toast_invite_check_unavailable);
    }
    const normalized = email.toLowerCase();
    if (!INVITE_EMAIL_PATTERN.test(normalized)) {
      return t(($) => $.members.toast_invalid_email);
    }
    if (members.some((m) => m.email.toLowerCase() === normalized)) {
      return t(($) => $.members.toast_already_member, { email });
    }
    if (invitations.some((inv) => inv.invitee_email.toLowerCase() === normalized)) {
      return t(($) => $.members.toast_invitation_pending, { email });
    }
    return null;
  };

  // The workspace is full: hold the invitation while Billing runs checkout
  // for Pro, which sends it once the new plan is confirmed.
  const handleUpgradeAndInvite = () => {
    if (!workspace || !navigation) return;
    const email = inviteEmail.trim();
    const problem = upgradeInviteProblem(email);
    if (problem) {
      toast.error(problem);
      return;
    }
    const token = saveUpgradeInvite(workspace.slug, { email, role: inviteRole });
    setInviteOpen(false);
    const href = settingsHref(navigation.pathname, navigation.searchParams, "billing");
    navigation.push(`${href}&upgrade=pro&held_invite=${encodeURIComponent(token)}`);
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
    void (purchase.promote
      ? changeRole(purchase.promote.memberId, purchase.role)
      : sendInvitation(purchase.email, purchase.role, purchase.projectIds))
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
    changeRole,
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
          // A revoked invitation frees its member slot.
          qc.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
          toast.success(t(($) => $.members.toast_invitation_revoked));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invitation_revoke_failed));
        } finally {
          setInvitationActionId(null);
        }
      },
    });
  };

  const applyRoleChange = async (member: MemberWithUser, role: MemberRole) => {
    setMemberActionId(member.id);
    try {
      await changeRole(member.id, role);
    } catch (e) {
      const promoting = member.role === "guest";
      if (promoting && isMemberLimitReachedError(e)) {
        // A member takes a slot on a plan that caps them; a guest does not.
        void qc.invalidateQueries({ queryKey: memberLimitKeys.all(wsId) });
        toast.error(t(($) => $.members.toast_member_limit_reached));
        return;
      }
      if (
        promoting &&
        (await handleSeatRefusal(e, {
          email: member.email,
          role,
          promote: { memberId: member.id, name: member.name },
        }))
      ) {
        return;
      }
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_role_failed));
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRoleChange = (member: MemberWithUser, role: MemberRole) => {
    if (!workspace) return;
    if (role === "guest") {
      // A guest needs projects to see, so demotion goes through the picker.
      setProjectsDialog({ member, mode: "demote" });
      return;
    }
    if (member.role === "guest") {
      setConfirmAction({
        title: t(($) => $.members.make_member_title, { name: member.name }),
        description: t(($) => $.members.make_member_description, { name: member.name }),
        actionLabel: role === "member" ? t(($) => $.members.make_member) : roleConfig[role].label,
        onConfirm: () => applyRoleChange(member, role),
      });
      return;
    }
    void applyRoleChange(member, role);
  };

  const handleProjectsConfirm = async (projectIds: string[]) => {
    if (!workspace || !projectsDialog) return;
    const { member, mode } = projectsDialog;
    try {
      if (mode === "demote") {
        await changeRole(member.id, "guest", projectIds);
      } else {
        await api.setMemberProjects(workspace.id, member.id, projectIds);
        await qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
        toast.success(t(($) => $.members.toast_projects_updated));
      }
      setProjectsDialog(null);
    } catch (e) {
      refreshFeaturesOnLockedError(qc, wsId, e);
      toast.error(paidFeatureError(e, e instanceof Error ? e.message : t(($) => $.members.toast_role_failed)));
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
      setShareLinkOpen(false);
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

  const roleItems = (["member", "admin"] as const).map((value) => ({
    value,
    label: roleConfig[value].label,
  }));
  const expiryOptions = [
    { value: "24", label: t(($) => $.members.expiry_24h) },
    { value: "168", label: t(($) => $.members.expiry_7d) },
    { value: "720", label: t(($) => $.members.expiry_30d) },
    { value: "0", label: t(($) => $.members.expiry_never) },
  ];
  const inviteRoles: MemberRole[] = guestsEnabled ? ["member", "admin", "guest"] : ["member", "admin"];
  const guestInviteLocked = inviteRole === "guest" && !canAddGuests;
  const views: { value: MembersView; label: string; count: number }[] = [
    {
      value: "members",
      label: showGuestViews ? t(($) => $.members.all_label) : t(($) => $.members.members_label),
      count: members.length,
    },
    ...(showGuestViews
      ? [
          {
            value: "seated" as const,
            label: t(($) => $.members.members_label),
            count: members.length - guestCount,
          },
          { value: "guests" as const, label: t(($) => $.members.guests_label), count: guestCount },
        ]
      : []),
    { value: "invitations", label: t(($) => $.members.pending_label), count: invitations.length },
    ...(canManageWorkspace
      ? [{ value: "links" as const, label: t(($) => $.members.share_links_label), count: shareLinks.length }]
      : []),
  ];
  const activeView = views.some((item) => item.value === view) ? view : "members";
  const isPeopleView = activeView === "members" || activeView === "seated" || activeView === "guests";

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.members)}
      scope="workspace"
      actions={
        canManageWorkspace ? (
          <Button onClick={() => setInviteOpen(true)}>
            <Plus />
            {t(($) => $.members.invite_title)}
          </Button>
        ) : undefined
      }
    >
      {currentMember && !canManageWorkspace ? (
        <SettingsReadOnlyNotice wsId={wsId} />
      ) : null}
      <section className="space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <SettingsViewTabs
            label={t(($) => $.page.tabs.members)}
            items={views}
            value={activeView}
            onChange={setView}
          />
          <div className="flex min-w-0 items-center gap-3">
            {enforcedMemberLimit && !seats ? (
              <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
                {t(($) => $.members.member_limit_usage, {
                  used: enforcedMemberLimit.used,
                  limit: enforcedMemberLimit.limit,
                })}
              </span>
            ) : null}
            {seats && billingEnabled ? (
              <span className="shrink-0 text-caption text-muted-foreground">
                {t(($) => $.members.seats_used, {
                  used: seats.used + seats.reserved,
                  purchased: seats.purchased,
                })}
                {" · "}
                {guestCount > 0 ? (
                  <>
                    {t(($) => $.members.guest_count, { count: guestCount })}
                    {" · "}
                  </>
                ) : null}
                <AppLink
                  href={
                    navigation
                      ? settingsHref(navigation.pathname, navigation.searchParams, "billing")
                      : "?tab=billing"
                  }
                  className="text-foreground underline-offset-4 hover:underline"
                >
                  {t(($) => $.members.manage_seats)}
                </AppLink>
              </span>
            ) : guestCount > 0 ? (
              <span className="shrink-0 text-caption text-muted-foreground">
                {t(($) => $.members.guest_count, { count: guestCount })}
              </span>
            ) : null}
            {isPeopleView ? (
              <div className="relative w-full sm:w-56">
                <Search
                  aria-hidden="true"
                  className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                />
                <Input
                  type="search"
                  value={memberQuery}
                  onChange={(event) => setMemberQuery(event.target.value)}
                  placeholder={t(($) => $.members.search_placeholder)}
                  aria-label={t(($) => $.members.search_placeholder)}
                  className="pl-8"
                />
              </div>
            ) : activeView === "links" ? (
              <Button variant="outline" size="sm" onClick={() => setShareLinkOpen(true)}>
                <Plus />
                {t(($) => $.members.share_links_create_title)}
              </Button>
            ) : null}
          </div>
        </div>

        {isPeopleView ? (
          visibleMembers.length > 0 ? (
            <SettingsCard>
              {visibleMembers.map((m) => (
                <div key={m.id}>
                  <MemberRow
                    member={m}
                    canManage={canManageWorkspace}
                    canManageOwners={isOwner}
                    guestsEnabled={canAddGuests}
                    projectNames={(m.project_ids ?? []).flatMap((id) => projectTitles.get(id) ?? [])}
                    ownerCount={ownerCount}
                    isSelf={m.user_id === user?.id}
                    busy={memberActionId === m.id}
                    onRoleChange={(role) => handleRoleChange(m, role)}
                    onManageProjects={() => setProjectsDialog({ member: m, mode: "edit" })}
                    onRemove={() => handleRemoveMember(m)}
                  />
                </div>
              ))}
            </SettingsCard>
          ) : (
            <ObjectSectionEmpty
              icon={needle ? Search : Users}
              description={needle ? t(($) => $.members.no_results) : t(($) => $.members.no_members)}
            />
          )
        ) : activeView === "invitations" ? (
          invitations.length > 0 ? (
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
          ) : (
            <ObjectSectionEmpty icon={Mail} description={t(($) => $.members.no_invitations)} />
          )
        ) : shareLinks.length > 0 ? (
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
        ) : (
          <ObjectSectionEmpty icon={Link} description={t(($) => $.members.no_share_links)} />
        )}
      </section>

      <Dialog
        open={inviteOpen}
        onOpenChange={(open) => {
          if (!open && !inviteLoading) setInviteOpen(false);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.members.invite_title)}</DialogTitle>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!inviteEmail.trim() || guestInviteMissingProjects || guestInviteLocked) return;
              if (inviteCanUpgrade) handleUpgradeAndInvite();
              else if (!inviteFull) void handleInviteMember();
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="invite-email">{t(($) => $.members.email_label)}</Label>
              <Input
                id="invite-email"
                type="email"
                name="invite-email"
                autoComplete="email"
                spellCheck={false}
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder={t(($) => $.members.invite_email_placeholder)}
                autoFocus
              />
            </div>
            <fieldset className="space-y-1.5">
              <legend className="mb-1.5 text-body leading-none font-medium">
                {t(($) => $.members.role_label)}
              </legend>
              <div className="space-y-2">
                {inviteRoles.map((role) => (
                  <label
                    key={role}
                    className={cn(
                      "flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 transition-colors",
                      inviteRole === role
                        ? "border-foreground"
                        : "border-border hover:bg-surface-hover",
                    )}
                  >
                    <input
                      type="radio"
                      name="invite-role"
                      value={role}
                      checked={inviteRole === role}
                      onChange={() => setInviteRole(role)}
                      className="mt-0.5 size-3.5 shrink-0 accent-foreground"
                    />
                    <span className="min-w-0">
                      <span className="flex items-center gap-2 text-body font-medium">
                        {roleConfig[role].label}
                        {role === "guest" ? (
                          <Badge variant="secondary" className="bg-success/10 text-success">
                            {t(($) => $.members.guest_free_tag)}
                          </Badge>
                        ) : null}
                      </span>
                      <span className="block text-caption text-muted-foreground">
                        {roleConfig[role].description}
                      </span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>
            {inviteRole === "guest" && guestAccess.state === "unavailable" ? (
              <PaidFeatureNotice state="unavailable" />
            ) : inviteRole === "guest" ? (
              <GuestProjectsField
                id="invite-guest-projects"
                projects={shareableProjects}
                value={inviteProjects}
                onChange={setInviteProjects}
              />
            ) : null}
            {inviteFull && enforcedMemberLimit ? (
              <Alert>
                <Users aria-hidden="true" />
                <AlertTitle>
                  {t(($) => $.members.member_limit_full_title, { limit: enforcedMemberLimit.limit })}
                </AlertTitle>
                <AlertDescription>
                  {!billingEnabled
                    ? t(($) => $.members.member_limit_full_description_no_upgrade)
                    : subscriptionNeedsAttention
                      ? t(($) => $.members.member_limit_full_description_billing)
                      : enforcedMemberLimit.pending > 0
                      ? t(($) => $.members.member_limit_full_description_pending, {
                          pending: enforcedMemberLimit.pending,
                        })
                      : t(($) => $.members.member_limit_full_description)}
                </AlertDescription>
              </Alert>
            ) : null}
            <DialogFooter className={enforcedMemberLimit ? "sm:items-center sm:justify-between" : undefined}>
              {enforcedMemberLimit ? (
                <span className="text-caption text-muted-foreground tabular-nums">
                  {t(($) => $.members.member_limit_usage, {
                    used: enforcedMemberLimit.used,
                    limit: enforcedMemberLimit.limit,
                  })}
                </span>
              ) : null}
              <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setInviteOpen(false)}
                  disabled={inviteLoading}
                >
                  {t(($) => $.members.confirm_cancel)}
                </Button>
                {inviteNeedsAttention ? (
                  <Button
                    type="button"
                    disabled={!navigation}
                    onClick={() => {
                      if (!navigation) return;
                      setInviteOpen(false);
                      navigation.push(settingsHref(navigation.pathname, navigation.searchParams, "billing"));
                    }}
                  >
                    {t(($) => $.members.open_billing)}
                  </Button>
                ) : inviteCanUpgrade ? (
                  <Button
                    type="button"
                    disabled={inviteLoading || !inviteEmail.trim() || !navigation || !inviteChecksReady}
                    onClick={handleUpgradeAndInvite}
                  >
                    {t(($) => $.members.upgrade_and_invite)}
                  </Button>
                ) : (
                  <Button
                    type="submit"
                    disabled={
                      inviteLoading ||
                      !inviteEmail.trim() ||
                      inviteFull ||
                      guestInviteMissingProjects ||
                      guestInviteLocked
                    }
                    aria-busy={inviteLoading || undefined}
                  >
                    {inviteLoading
                      ? t(($) => $.members.inviting)
                      : inviteRole === "guest"
                        ? t(($) => $.members.invite_guest_button)
                        : t(($) => $.members.invite_button)}
                  </Button>
                )}
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={shareLinkOpen}
        onOpenChange={(open) => {
          if (!open && !shareLinkLoading) setShareLinkOpen(false);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.members.share_links_create_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.members.share_links_description)}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="share-link-role">{t(($) => $.members.role_label)}</Label>
              <Select
                items={roleItems}
                value={shareLinkRole}
                onValueChange={(value) => setShareLinkRole(value as MemberRole)}
              >
                <SelectTrigger id="share-link-role" className="w-full">
                  <SelectValue>{() => roleConfig[shareLinkRole].label}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {roleItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="share-link-expiry">{t(($) => $.members.expiry_label)}</Label>
              <Select
                items={expiryOptions}
                value={shareLinkExpiry}
                onValueChange={(v) => v && setShareLinkExpiry(v)}
              >
                <SelectTrigger id="share-link-expiry" className="w-full">
                  <SelectValue>
                    {() =>
                      expiryOptions.find((option) => option.value === shareLinkExpiry)?.label ??
                      shareLinkExpiry
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {expiryOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShareLinkOpen(false)}
              disabled={shareLinkLoading}
            >
              {t(($) => $.members.confirm_cancel)}
            </Button>
            <Button
              onClick={() => void handleCreateShareLink()}
              disabled={shareLinkLoading}
              aria-busy={shareLinkLoading || undefined}
            >
              {shareLinkLoading
                ? t(($) => $.members.share_links_creating)
                : t(($) => $.members.share_links_create_button)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
              {inviteSeatPurchase?.promote
                ? t(($) => $.members.make_member_title, { name: inviteSeatPurchase.promote.name })
                : t(($) => $.members.seat_purchase_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {inviteSeatPurchase?.phase === "review"
                ? inviteSeatPurchase.promote
                  ? t(($) => $.members.make_member_seat_description, {
                      name: inviteSeatPurchase.promote.name,
                    })
                  : t(($) => $.members.seat_purchase_description, {
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
                ? inviteSeatPurchase.promote
                  ? t(($) => $.members.making_member)
                  : t(($) => $.members.inviting)
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
                {inviteSeatPurchase.promote
                  ? t(($) => $.members.purchase_seat_and_make_member)
                  : t(($) => $.members.purchase_seat_and_invite)}
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

      <GuestProjectsDialog
        open={projectsDialog !== null}
        title={
          projectsDialog?.mode === "demote"
            ? t(($) => $.members.make_guest_title, { name: projectsDialog.member.name })
            : t(($) => $.members.manage_projects_title, { name: projectsDialog?.member.name ?? "" })
        }
        description={
          projectsDialog?.mode === "demote"
            ? t(($) => $.members.make_guest_description, { name: projectsDialog.member.name })
            : undefined
        }
        confirmLabel={
          projectsDialog?.mode === "demote"
            ? t(($) => $.members.make_guest_button)
            : t(($) => $.members.save_projects)
        }
        projects={shareableProjects}
        initial={projectsDialog?.member.project_ids ?? []}
        onOpenChange={(open) => {
          if (!open) setProjectsDialog(null);
        }}
        onConfirm={handleProjectsConfirm}
      />

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
              {confirmAction?.actionLabel ?? t(($) => $.members.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

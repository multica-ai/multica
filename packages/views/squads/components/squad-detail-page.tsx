"use client";

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { isImeComposing } from "@multica/core/utils";
import { getShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { agentListOptions, memberListOptions, squadMemberStatusOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { AppLink, currentPath, useNavigation } from "../../navigation";
import {
  OBJECT_PAGE_BODY,
  ObjectIdentity,
  ObjectPage,
  ObjectPanelSection,
  ObjectProperty,
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
} from "../../layout/object-page";
import { CollectionPageState } from "../../layout/collection-page";
import {
  Bot,
  ChevronDown,
  Crown,
  Globe,
  Link2,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  Save,
  Trash2,
  UserPlus,
  Users,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { ContentEditor } from "../../editor/content-editor";
import { ReadonlyContent } from "../../editor/readonly-content";
import { StatusIcon } from "../../issues/components/status-icon";
import { useStatusLabel } from "../../issues/utils/status-label";
import {
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../../issues/components/pickers/property-picker";
import { toast } from "sonner";
import type { Issue, Squad, SquadMember, SquadMemberStatus, Agent, MemberWithUser } from "@multica/core/types";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

const RECENT_WORK_LIMIT = 5;

export function SquadDetailPage() {
  const { t } = useT("squads");
  const { t: tLayout } = useT("layout");
  const { t: tCommon } = useT("common");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const router = useNavigation();
  const { pathname, push } = router;
  const queryClient = useQueryClient();
  const squadId = pathname.split("/").pop() ?? "";

  const { data: squad, isError: squadError, refetch: refetchSquad } = useQuery<Squad>({
    queryKey: [...workspaceKeys.squads(wsId), squadId],
    queryFn: () => api.getSquad(squadId),
    enabled: !!workspace?.id && !!squadId,
  });

  const { data: members = [], refetch: refetchMembers } = useQuery<SquadMember[]>({
    queryKey: [...workspaceKeys.squads(wsId), squadId, "members"],
    queryFn: () => api.listSquadMembers(squadId),
    enabled: !!workspace?.id && !!squadId,
  });

  // Per-squad working/idle/offline + active-issue snapshot. WS task / agent /
  // daemon events invalidate this via use-realtime-sync; the staleTime is a
  // tab-focus safety net. Indexed by member_id so each member row can look up
  // its status in O(1).
  const { data: memberStatusResp } = useQuery({
    ...squadMemberStatusOptions(wsId, squadId),
    enabled: !!workspace?.id && !!squadId,
  });
  const memberStatusById = useMemo(() => {
    const map = new Map<string, SquadMemberStatus>();
    for (const s of memberStatusResp?.members ?? []) map.set(s.member_id, s);
    return map;
  }, [memberStatusResp]);

  const { data: recentWork, isPending: recentWorkPending } = useQuery({
    queryKey: [...workspaceKeys.squads(wsId), squadId, "recent-issues"],
    queryFn: () =>
      api.listIssues({
        assignee_filters: [{ type: "squad", id: squadId }],
        sort_by: "updated_at",
        sort_direction: "desc",
        limit: RECENT_WORK_LIMIT,
      }),
    enabled: !!workspace?.id && !!squadId,
  });

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: wsMembers = [] } = useQuery(memberListOptions(wsId));

  const currentUser = useAuthStore((s) => s.user);
  const myRole = useMemo(() => {
    if (!currentUser) return null;
    return wsMembers.find((m) => m.user_id === currentUser.id)?.role ?? null;
  }, [wsMembers, currentUser]);
  const isWorkspaceAdmin = myRole === "owner" || myRole === "admin";
  // Per-squad management gate: workspace owner/admin manage every squad; the
  // creator manages the squads they created. Mirrors canManageSquad in
  // server/internal/handler/squad.go so editable controls appear exactly when
  // the API will accept the write, and everyone else gets a read-only view
  // instead of controls that 403 (MUL-4223).
  const canManage =
    isWorkspaceAdmin || (!!currentUser && squad?.creator_id === currentUser.id);

  const [showAddMember, setShowAddMember] = useState(false);
  const [confirmArchive, setConfirmArchive] = useState(false);

  const updateSquadMut = useMutation({
    mutationFn: (data: { name?: string; description?: string; instructions?: string; avatar_url?: string; leader_id?: string }) => api.updateSquad(squadId, data),
    onSuccess: () => {
      refetchSquad();
      refetchMembers();
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
    },
  });

  const addMemberMut = useMutation({
    mutationFn: (input: { type: "agent" | "member"; id: string; role?: string }) =>
      api.addSquadMember(squadId, {
        member_type: input.type,
        member_id: input.id,
        role: input.role?.trim() || undefined,
      }),
    onSuccess: () => { refetchMembers(); toast.success(t(($) => $.toasts.member_added)); },
    onError: (err) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toasts.member_add_failed)),
  });

  const removeMemberMut = useMutation({
    mutationFn: (m: SquadMember) => api.removeSquadMember(squadId, { member_type: m.member_type, member_id: m.member_id }),
    onSuccess: () => { refetchMembers(); toast.success(t(($) => $.toasts.member_removed)); },
    onError: (err) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toasts.member_remove_failed)),
  });

  const updateRoleMut = useMutation({
    mutationFn: (input: { member: SquadMember; role: string }) =>
      api.updateSquadMemberRole(squadId, {
        member_type: input.member.member_type,
        member_id: input.member.member_id,
        role: input.role,
      }),
    onSuccess: () => { refetchMembers(); toast.success(t(($) => $.toasts.role_updated)); },
    onError: (err) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toasts.role_update_failed)),
  });

  const setLeaderMut = useMutation({
    mutationFn: (agentId: string) => api.updateSquad(squadId, { leader_id: agentId }),
    onSuccess: () => {
      refetchSquad();
      refetchMembers();
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      toast.success(t(($) => $.toasts.leader_updated));
    },
    onError: (err) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toasts.leader_update_failed)),
  });

  const deleteMut = useMutation({
    mutationFn: () => api.deleteSquad(squadId),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) }); push(p.squads()); toast.success(t(($) => $.archive_dialog.success)); },
    onError: (err) =>
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toasts.archive_failed)),
  });

  const getEntityName = (type: string, id: string) => {
    if (type === "agent") return agents.find((a: Agent) => a.id === id)?.name ?? id.slice(0, 8);
    return wsMembers.find((m) => m.user_id === id)?.name ?? id.slice(0, 8);
  };

  const segments = [{ href: p.squads(), label: t(($) => $.page.title) }];
  const layoutId = "multica_squad_detail_layout";

  if (!squad) {
    if (squadError) {
      return (
        <ObjectPage layoutId={layoutId} segments={segments} leaf={null}>
          <CollectionPageState icon={Users} title={t(($) => $.detail.not_found)} />
        </ObjectPage>
      );
    }
    return (
      <ObjectPage
        layoutId={layoutId}
        segments={segments}
        leaf={<Skeleton className="h-4 w-32" />}
        panel={<SquadPanelSkeleton />}
      >
        <SquadBodySkeleton />
      </ObjectPage>
    );
  }

  const availableAgents = agents.filter((a: Agent) => !a.archived_at && !members.some((m) => m.member_type === "agent" && m.member_id === a.id));
  const availableMembers = wsMembers.filter((m) => !members.some((sm) => sm.member_type === "member" && sm.member_id === m.user_id));
  const isLeader = (m: SquadMember) => m.member_type === "agent" && squad.leader_id === m.member_id;
  const isArchived = (m: SquadMember) =>
    m.member_type === "agent" && !!agents.find((a: Agent) => a.id === m.member_id)?.archived_at;
  const archived = !!squad.archived_at;
  const createAgentHref = `${p.newAgent()}?squad=${encodeURIComponent(squadId)}`;

  const assignWork = () =>
    useModalStore.getState().open("create-issue", {
      assignee_type: "squad",
      assignee_id: squad.id,
    });

  const copyLink = () => {
    void copyText(router.getShareableUrl(currentPath(router))).then((ok) => {
      if (ok) toast.success(t(($) => $.detail.link_copied));
    });
  };

  return (
    <ObjectPage
      layoutId={layoutId}
      segments={segments}
      leaf={
        <span className="flex min-w-0 items-center gap-1.5">
          <SquadAvatar squad={squad} size="xs" />
          <span className="truncate font-medium text-foreground">{squad.name}</span>
        </span>
      }
      actions={
        <>
          {!archived && (
            <Button size="sm" onClick={assignWork}>
              <Plus className="size-3.5" />
              {t(($) => $.detail.assign_work)}
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <IconButton
                  label={tCommon(($) => $.more_actions)}
                  tooltipSide="bottom"
                  className="text-muted-foreground"
                />
              }
            >
              <MoreHorizontal />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto min-w-44">
              <DropdownMenuItem onClick={copyLink}>
                <Link2 className="size-3.5" />
                {t(($) => $.detail.copy_link)}
              </DropdownMenuItem>
              {canManage && !archived && (
                <>
                  <DropdownMenuItem onClick={() => push(createAgentHref)}>
                    <Bot className="size-3.5" />
                    {t(($) => $.members_tab.create_agent_button)}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => setConfirmArchive(true)}>
                    <Trash2 className="size-3.5" />
                    {t(($) => $.page.archive_action)}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      }
      panel={
        <ObjectPanelSection title={tLayout(($) => $.object_page.properties)}>
          <SquadProperties
            squad={squad}
            members={members}
            canManage={canManage && !archived}
            isArchived={isArchived}
            getEntityName={getEntityName}
            onSetLeader={(id) => setLeaderMut.mutate(id)}
            setLeaderPending={setLeaderMut.isPending}
          />
        </ObjectPanelSection>
      }
    >
      <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
        <ObjectIdentity
          media={
            canManage ? (
              <AvatarUploadControl
                variant="squad"
                value={squad.avatar_url ?? null}
                name={squad.name}
                size={48}
                onUploaded={(url) => updateSquadMut.mutateAsync({ avatar_url: url })}
              />
            ) : (
              <SquadAvatar squad={squad} size="identity" />
            )
          }
          title={
            canManage ? (
              <SquadNameEditor
                value={squad.name}
                onSave={async (next) => { await updateSquadMut.mutateAsync({ name: next.trim() }); }}
              />
            ) : (
              squad.name
            )
          }
          description={
            canManage ? (
              <SquadDescriptionEditor
                value={squad.description ?? ""}
                onSave={async (next) => { await updateSquadMut.mutateAsync({ description: next }); }}
              />
            ) : (
              squad.description || undefined
            )
          }
          trailing={
            archived ? (
              <span className="shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-caption text-muted-foreground">
                {t(($) => $.profile_card.archived)}
              </span>
            ) : undefined
          }
        />

        <SquadMembersSection
          members={members}
          memberStatusById={memberStatusById}
          canManage={canManage && !archived}
          isLeader={isLeader}
          isArchived={isArchived}
          getEntityName={getEntityName}
          onAddMemberClick={() => setShowAddMember(true)}
          onSetLeader={(id) => setLeaderMut.mutate(id)}
          onRemoveMember={(m) => removeMemberMut.mutate(m)}
          onUpdateRole={async (m, role) => { await updateRoleMut.mutateAsync({ member: m, role }); }}
          setLeaderPending={setLeaderMut.isPending}
        />

        <SquadInstructionsSection
          squad={squad}
          canManage={canManage && !archived}
          onSave={async (next) => { await updateSquadMut.mutateAsync({ instructions: next }); toast.success(t(($) => $.toasts.instructions_saved)); }}
        />

        <SquadRecentWorkSection
          issues={recentWork?.issues}
          loading={recentWorkPending}
        />
      </div>

      {showAddMember && (
        <AddMemberDialog
          availableMembers={availableMembers}
          availableAgents={availableAgents}
          onClose={() => setShowAddMember(false)}
          onSubmit={async (input) => { await addMemberMut.mutateAsync(input); }}
        />
      )}

      {confirmArchive && (
        <AlertDialog
          open
          onOpenChange={(v) => { if (!v && !deleteMut.isPending) setConfirmArchive(false); }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.archive_dialog.title)}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.archive_dialog.description, { name: squad.name })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={deleteMut.isPending}>
                {t(($) => $.archive_dialog.cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                onClick={() => deleteMut.mutate()}
                disabled={deleteMut.isPending}
                className="bg-destructive text-white hover:bg-destructive/90"
              >
                {deleteMut.isPending
                  ? t(($) => $.archive_dialog.archiving)
                  : t(($) => $.archive_dialog.confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </ObjectPage>
  );
}

function SquadBodySkeleton() {
  return (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-12 rounded-full" />
        <div className="space-y-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-3 w-64" />
        </div>
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-36 w-full rounded-lg" />
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-16 w-full rounded-lg" />
      </div>
    </div>
  );
}

function SquadPanelSkeleton() {
  return (
    <div className="space-y-3 px-2 pt-1">
      <Skeleton className="h-3 w-20" />
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-4 w-full" />
      ))}
    </div>
  );
}

// The squad's avatar, or the squad glyph when none is set. "identity" is the
// 48px mark at the top of the body.
function SquadAvatar({ squad, size }: { squad: Squad; size: "xs" | "identity" }) {
  const initials = squad.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
  return (
    <span className={cn("inline-flex shrink-0", size === "identity" && "[--avatar-slot-size:3rem]")}>
      <ActorAvatarBase
        name={squad.name}
        initials={initials}
        avatarUrl={squad.avatar_url ? resolvePublicFileUrl(squad.avatar_url) : null}
        isSquad
        size={size === "identity" ? "2xl" : "xs"}
      />
    </span>
  );
}

const SQUAD_STATUS_LABEL_KEYS = {
  working: "status_working",
  idle: "status_idle",
  offline: "status_offline",
  unstable: "status_unstable",
  archived: "status_archived",
} as const;

function SquadMembersSection({
  members,
  memberStatusById,
  canManage,
  isLeader,
  isArchived,
  getEntityName,
  onAddMemberClick,
  onSetLeader,
  onRemoveMember,
  onUpdateRole,
  setLeaderPending,
}: {
  members: SquadMember[];
  memberStatusById: Map<string, SquadMemberStatus>;
  // When false, add/leader/remove controls and role editing are hidden; the
  // roster stays visible and read-only. See canManageSquad in
  // server/internal/handler/squad.go.
  canManage: boolean;
  isLeader: (m: SquadMember) => boolean;
  isArchived: (m: SquadMember) => boolean;
  getEntityName: (type: string, id: string) => string;
  onAddMemberClick: () => void;
  onSetLeader: (agentId: string) => void;
  onRemoveMember: (m: SquadMember) => void;
  onUpdateRole: (m: SquadMember, role: string) => Promise<void>;
  setLeaderPending: boolean;
}) {
  const { t } = useT("squads");
  const timeAgo = useTimeAgo();
  const p = useWorkspacePaths();
  const { push } = useNavigation();

  return (
    <ObjectSection
      title={t(($) => $.members_tab.section_title)}
      count={members.length}
      action={
        canManage ? (
          <ObjectSectionAction icon={UserPlus} onClick={onAddMemberClick}>
            {t(($) => $.members_tab.add_member_button)}
          </ObjectSectionAction>
        ) : null
      }
    >
      {members.length > 0 ? (
        <ObjectSectionCard>
          {members.map((m) => {
            const name = getEntityName(m.member_type, m.member_id);
            const leader = isLeader(m);
            const status = memberStatusById.get(m.member_id);
            const statusValue = status?.status ?? null;
            const statusLabel =
              statusValue && statusValue in SQUAD_STATUS_LABEL_KEYS
                ? t(($) => $.members_tab[SQUAD_STATUS_LABEL_KEYS[statusValue as keyof typeof SQUAD_STATUS_LABEL_KEYS]])
                : null;
            const activeIssues = status?.active_issues ?? [];
            const primaryIssue = activeIssues[0];
            const extraIssueCount = Math.max(0, activeIssues.length - 1);
            const canMakeLeader = canManage && m.member_type === "agent" && !leader && !isArchived(m);
            const canRemove = canManage && !leader;
            const hasMenu = m.member_type === "agent" || canMakeLeader || canRemove;
            return (
              <div key={m.id} className="group/member flex min-h-11 items-center gap-3 px-3 py-1.5 text-body">
                <ActorAvatar
                  actorType={m.member_type}
                  actorId={m.member_id}
                  size="md"
                  showStatusDot
                  enableHoverCard={m.member_type === "agent"}
                  hoverCardVariant="live"
                />
                <div className="flex min-w-0 flex-1 items-center gap-2">
                  <span className="truncate font-medium">{name}</span>
                  {leader && (
                    <span className="inline-flex shrink-0 items-center gap-0.5 rounded-sm border px-1 text-caption text-muted-foreground">
                      <Crown className="size-3" />
                      {t(($) => $.members_tab.leader_chip)}
                    </span>
                  )}
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {t(($) => $.member_type[m.member_type])}
                  </span>
                  {canManage ? (
                    <RoleEditor
                      value={m.role ?? ""}
                      onSave={async (next) => { await onUpdateRole(m, next); }}
                    />
                  ) : m.role ? (
                    <span className="min-w-0 truncate text-caption text-muted-foreground">· {m.role}</span>
                  ) : null}
                </div>
                {m.member_type === "agent" && (
                  <div className="flex min-w-0 max-w-[45%] shrink-0 items-center gap-1 text-caption text-muted-foreground">
                    {statusValue === "working" && primaryIssue ? (
                      <>
                        <span className="shrink-0">{t(($) => $.detail.working_on)}</span>
                        <AppLink
                          href={p.issueDetail(primaryIssue.issue_id)}
                          title={primaryIssue.title}
                          className="shrink-0 font-mono uppercase transition-colors hover:text-foreground"
                        >
                          {primaryIssue.identifier}
                        </AppLink>
                        {primaryIssue.issue_status === "blocked" && (
                          <span className="shrink-0 text-micro uppercase tracking-wide text-warning">
                            {t(($) => $.members_tab.issue_status_blocked)}
                          </span>
                        )}
                        {extraIssueCount > 0 && (
                          <span className="truncate">
                            · {t(($) => $.members_tab.active_issue_more, { count: extraIssueCount })}
                          </span>
                        )}
                      </>
                    ) : statusLabel ? (
                      <span className="truncate">
                        {statusLabel}
                        {statusValue !== "working" && status?.last_active_at
                          ? ` · ${timeAgo(status.last_active_at)}`
                          : null}
                      </span>
                    ) : null}
                  </div>
                )}
                {hasMenu ? (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <IconButton
                          label={t(($) => $.detail.member_menu, { name })}
                          size="icon-xs"
                          className="text-muted-foreground opacity-0 group-hover/member:opacity-100 focus-visible:opacity-100 data-[popup-open]:opacity-100 [@media(pointer:coarse)]:opacity-100"
                        />
                      }
                    >
                      <MoreHorizontal />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-auto min-w-44">
                      {m.member_type === "agent" && (
                        <DropdownMenuItem onClick={() => push(p.agentDetail(m.member_id))}>
                          <Bot className="size-3.5" />
                          {t(($) => $.members_tab.view_agent_tooltip)}
                        </DropdownMenuItem>
                      )}
                      {canMakeLeader && (
                        <DropdownMenuItem disabled={setLeaderPending} onClick={() => onSetLeader(m.member_id)}>
                          <Crown className="size-3.5" />
                          {t(($) => $.members_tab.make_leader_tooltip)}
                        </DropdownMenuItem>
                      )}
                      {canRemove && (
                        <>
                          {m.member_type === "agent" && <DropdownMenuSeparator />}
                          <DropdownMenuItem variant="destructive" onClick={() => onRemoveMember(m)}>
                            <Trash2 className="size-3.5" />
                            {t(($) => $.members_tab.remove_member_tooltip)}
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                ) : (
                  <span aria-hidden="true" className="size-6 shrink-0" />
                )}
              </div>
            );
          })}
        </ObjectSectionCard>
      ) : null}
    </ObjectSection>
  );
}

// The squad leader's prompt picks these up at task claim time
// (server/internal/handler/daemon.go).
function SquadInstructionsSection({
  squad,
  canManage,
  onSave,
}: {
  squad: Squad;
  canManage: boolean;
  onSave: (instructions: string) => Promise<void>;
}) {
  const { t } = useT("squads");
  const saved = squad.instructions ?? "";
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(saved);
  const [saving, setSaving] = useState(false);
  const isDirty = value !== saved;

  useEffect(() => {
    if (!editing) setValue(saved);
  }, [saved, editing]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(value);
      setEditing(false);
    } catch {
      // toast handled by parent
    } finally {
      setSaving(false);
    }
  };

  return (
    <ObjectSection
      title={t(($) => $.detail.instructions)}
      action={
        canManage && !editing ? (
          <ObjectSectionAction icon={Pencil} onClick={() => setEditing(true)}>
            {t(($) => $.detail.edit)}
          </ObjectSectionAction>
        ) : null
      }
    >
      {editing ? (
        <div className="space-y-2">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.instructions_tab.description)}
          </p>
          <div className="rounded-lg border bg-card px-3 py-2.5 transition-colors focus-within:border-input">
            <ContentEditor
              key={squad.id}
              value={value}
              onUpdate={setValue}
              placeholder={t(($) => $.instructions_tab.placeholder)}
              debounceMs={150}
              disableMentions
              className="min-h-24"
            />
          </div>
          <div className="flex items-center justify-end gap-2">
            {isDirty && (
              <span className="text-caption text-muted-foreground">{t(($) => $.instructions_tab.unsaved_changes)}</span>
            )}
            <Button
              variant="ghost"
              size="sm"
              disabled={saving}
              onClick={() => {
                setValue(saved);
                setEditing(false);
              }}
            >
              {t(($) => $.detail.cancel)}
            </Button>
            <Button size="sm" onClick={() => void handleSave()} disabled={!isDirty || saving}>
              {saving ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Save className="size-3.5" />
              )}
              {t(($) => $.instructions_tab.save_button)}
            </Button>
          </div>
        </div>
      ) : saved ? (
        <ObjectSectionCard className="px-3 py-2.5 text-body">
          <ReadonlyContent content={saved} />
        </ObjectSectionCard>
      ) : (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.instructions_tab.description)}
        </p>
      )}
    </ObjectSection>
  );
}

function SquadRecentWorkSection({
  issues,
  loading,
}: {
  issues: Issue[] | undefined;
  loading: boolean;
}) {
  const { t } = useT("squads");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);

  return (
    <ObjectSection title={t(($) => $.detail.recent_work)}>
      {loading ? (
        <Skeleton className="h-20 w-full rounded-lg" />
      ) : issues && issues.length > 0 ? (
        <ObjectSectionCard>
          {issues.map((issue) => (
            <AppLink
              key={issue.id}
              href={p.issueDetail(issue.id)}
              className="flex h-10 items-center gap-3 px-3 text-body transition-colors hover:bg-accent/50"
            >
              <StatusIcon
                status={issue.status}
                category={categoryOf(issue.status)}
                color={colorOf(issue.status)}
                icon={iconOf(issue.status)}
                className="size-3.5"
              />
              <span className="shrink-0 font-mono text-caption text-muted-foreground">{issue.identifier}</span>
              <span className="min-w-0 flex-1 truncate">{issue.title}</span>
              <span className="shrink-0 text-caption text-muted-foreground">
                {statusLabel(issue.status)} · {timeAgo(issue.updated_at)}
              </span>
            </AppLink>
          ))}
        </ObjectSectionCard>
      ) : (
        <p className="text-caption text-muted-foreground">{t(($) => $.detail.recent_work_empty)}</p>
      )}
    </ObjectSection>
  );
}

function SquadProperties({
  squad,
  members,
  canManage,
  isArchived,
  getEntityName,
  onSetLeader,
  setLeaderPending,
}: {
  squad: Squad;
  members: SquadMember[];
  canManage: boolean;
  isArchived: (m: SquadMember) => boolean;
  getEntityName: (type: string, id: string) => string;
  onSetLeader: (agentId: string) => void;
  setLeaderPending: boolean;
}) {
  const { t } = useT("squads");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const leaderName = getEntityName("agent", squad.leader_id);
  const leaderCandidates = members.filter((m) => m.member_type === "agent" && !isArchived(m));
  const leaderValue = (
    <>
      <ActorAvatar actorType="agent" actorId={squad.leader_id} size="xs" />
      <span className="truncate">{leaderName}</span>
    </>
  );

  return (
    <>
      <ObjectProperty label={t(($) => $.details.leader)}>
        {canManage ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              disabled={setLeaderPending}
              render={
                <button
                  type="button"
                  className="inline-flex min-w-0 items-center gap-1.5 text-caption transition-colors hover:text-foreground"
                />
              }
            >
              {leaderValue}
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-auto min-w-48">
              <DropdownMenuRadioGroup
                value={squad.leader_id}
                onValueChange={(id) => {
                  if (id !== squad.leader_id) onSetLeader(id as string);
                }}
              >
                {leaderCandidates.map((m) => (
                  <DropdownMenuRadioItem key={m.member_id} value={m.member_id}>
                    <ActorAvatar actorType="agent" actorId={m.member_id} size="xs" />
                    <span className="truncate">{getEntityName("agent", m.member_id)}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          leaderValue
        )}
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.details.members)}>
        {members.length > 0 && (
          <span className="flex shrink-0 items-center -space-x-1">
            {members.slice(0, 4).map((m) => (
              <span key={m.id} className="inline-flex rounded-full ring-2 ring-background">
                <ActorAvatar actorType={m.member_type} actorId={m.member_id} size="xs" />
              </span>
            ))}
          </span>
        )}
        <span className="truncate text-muted-foreground">
          {t(($) => $.profile_card.member_count, { count: members.length })}
        </span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.details.visibility)}>
        <Globe className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{t(($) => $.details.visibility_workspace)}</span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.details.created_by)}>
        <ActorAvatar actorType="member" actorId={squad.creator_id} size="xs" />
        <span className="truncate">{getEntityName("member", squad.creator_id)}</span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.details.created)}>
        <span className="text-muted-foreground">
          {new Date(squad.created_at).toLocaleDateString(locale, {
            year: "numeric",
            month: "short",
            day: "numeric",
          })}
        </span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.details.updated)}>
        <span className="text-muted-foreground">{timeAgo(squad.updated_at)}</span>
      </ObjectProperty>
    </>
  );
}

// Inline name editor — reveals a Pencil affordance on hover, opens a small
// popover with a single-line input. Mirrors the NameAndDescription editor
// in the agent inspector.
function SquadNameEditor({
  value,
  onSave,
}: {
  value: string;
  onSave: (next: string) => Promise<void>;
}) {
  const { t } = useT("squads");
  return (
    <InlineEditPopover
      value={value}
      onSave={onSave}
      title={t(($) => $.name_editor.title)}
      placeholder={t(($) => $.name_editor.placeholder)}
      validate={(v) =>
        v.trim().length > 0 ? null : t(($) => $.name_editor.required)
      }
    >
      {(triggerProps) => (
        <button
          type="button"
          {...triggerProps}
          className="group -mx-1 inline-flex items-center gap-1.5 self-start rounded-xs px-1 text-left text-title font-semibold leading-tight transition-colors hover:bg-accent/50"
        >
          <span>{value}</span>
          <Pencil className="h-3.5 w-3.5 shrink-0 text-transparent transition-colors group-hover:text-muted-foreground" />
        </button>
      )}
    </InlineEditPopover>
  );
}

function InlineEditPopover({
  value,
  onSave,
  title,
  placeholder,
  validate,
  children,
}: {
  value: string;
  onSave: (next: string) => Promise<void>;
  title: string;
  placeholder?: string;
  validate?: (v: string) => string | null;
  children: (triggerProps: { onClick: (e: React.MouseEvent) => void }) => ReactNode;
}) {
  const { t } = useT("squads");
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setDraft(value);
      setError(null);
    }
  }, [open, value]);

  const commit = async () => {
    const err = validate?.(draft) ?? null;
    if (err) {
      setError(err);
      return;
    }
    if (draft === value) {
      setOpen(false);
      return;
    }
    setSaving(true);
    try {
      await onSave(draft);
      setOpen(false);
      toast.success(t(($) => $.name_editor.saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.name_editor.save_failed));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={children({ onClick: () => setOpen(true) }) as React.ReactElement}
      />
      <PopoverContent align="start" className="w-72 p-3">
        <div className="space-y-2">
          <p className="text-caption font-medium">{title}</p>
          <Input
            autoFocus
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value);
              if (error) setError(null);
            }}
            placeholder={placeholder}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                setOpen(false);
                return;
              }
              if (isImeComposing(e)) return;
              if (e.key === "Enter") {
                e.preventDefault();
                void commit();
              }
            }}
            className="h-8"
          />
          {error && <p className="text-caption text-destructive">{error}</p>}
          <div className="flex items-center justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setOpen(false)} disabled={saving}>
              {t(($) => $.name_editor.cancel)}
            </Button>
            <Button size="sm" onClick={() => void commit()} disabled={saving || draft === value}>
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t(($) => $.name_editor.save)}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// Two-step add-member dialog (mirrors CreateAgentDialog's compact layout):
// 1) pick a target — Members + Agents in one searchable popover, each row
//    with an avatar so visual recognition matches the issue assignee picker;
// 2) optionally describe the role they'll play in this squad. Description
//    lives here (not on the picker) because role is per-squad context that
//    only makes sense at the moment of joining.
function AddMemberDialog({
  availableMembers,
  availableAgents,
  onClose,
  onSubmit,
}: {
  availableMembers: MemberWithUser[];
  availableAgents: Agent[];
  onClose: () => void;
  onSubmit: (input: { type: "agent" | "member"; id: string; role?: string }) => Promise<void>;
}) {
  const { t } = useT("squads");
  const [target, setTarget] = useState<{ type: "agent" | "member"; id: string; name: string } | null>(null);
  const [role, setRole] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerFilter, setPickerFilter] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const query = pickerFilter.trim().toLowerCase();
  const filteredMembers = availableMembers.filter((m) => m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query));
  const filteredAgents = availableAgents.filter((a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query));

  const canSubmit = !!target && !submitting;

  const handleSubmit = async () => {
    if (!target) return;
    setSubmitting(true);
    try {
      await onSubmit({ type: target.type, id: target.id, role });
      onClose();
    } catch {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.add_member_dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.add_member_dialog.description)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-caption text-muted-foreground">{t(($) => $.add_member_dialog.label_member)}</Label>
            <Popover open={pickerOpen} onOpenChange={(v) => { setPickerOpen(v); if (!v) setPickerFilter(""); }}>
              <PopoverTrigger className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1 text-left text-body transition-colors hover:bg-muted">
                {target ? (
                  <ActorAvatar actorType={target.type} actorId={target.id} size="sm" />
                ) : (
                  <UserPlus className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">
                    {target?.name ?? t(($) => $.add_member_dialog.select_target)}
                  </div>
                  {target && (
                    <div className="truncate text-caption text-muted-foreground">
                      {t(($) => $.member_type[target.type])}
                    </div>
                  )}
                </div>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${pickerOpen ? "rotate-180" : ""}`} />
              </PopoverTrigger>
              <PopoverContent align="start" className="w-[var(--anchor-width)] p-0">
                <div className="px-2 py-1.5 border-b">
                  <input
                    autoFocus
                    type="text"
                    value={pickerFilter}
                    onChange={(e) => setPickerFilter(e.target.value)}
                    placeholder={t(($) => $.add_member_dialog.search_placeholder)}
                    className="w-full bg-transparent text-body placeholder:text-muted-foreground outline-none"
                  />
                </div>
                <div className="p-1 max-h-72 overflow-y-auto">
                  {filteredMembers.length > 0 && (
                    <PickerSection label={t(($) => $.add_member_dialog.members_section)}>
                      {filteredMembers.map((m) => (
                        <PickerItem
                          key={m.user_id}
                          selected={target?.type === "member" && target.id === m.user_id}
                          onClick={() => {
                            setTarget({ type: "member", id: m.user_id, name: m.name });
                            setPickerOpen(false);
                            setPickerFilter("");
                          }}
                        >
                          <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
                          <span>{m.name}</span>
                        </PickerItem>
                      ))}
                    </PickerSection>
                  )}
                  {filteredAgents.length > 0 && (
                    <PickerSection label={t(($) => $.add_member_dialog.agents_section)}>
                      {filteredAgents.map((a) => (
                        <PickerItem
                          key={a.id}
                          selected={target?.type === "agent" && target.id === a.id}
                          onClick={() => {
                            setTarget({ type: "agent", id: a.id, name: a.name });
                            setPickerOpen(false);
                            setPickerFilter("");
                          }}
                        >
                          <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
                          <span>{a.name}</span>
                        </PickerItem>
                      ))}
                    </PickerSection>
                  )}
                  {filteredMembers.length === 0 && filteredAgents.length === 0 && <PickerEmpty />}
                </div>
              </PopoverContent>
            </Popover>
          </div>

          <div>
            <Label className="text-caption text-muted-foreground">
              {t(($) => $.add_member_dialog.label_role)}{" "}
              <span className="text-muted-foreground">{t(($) => $.add_member_dialog.label_optional)}</span>
            </Label>
            <Input
              type="text"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder={t(($) => $.add_member_dialog.role_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter" && canSubmit) void handleSubmit();
              }}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{t(($) => $.add_member_dialog.cancel)}</Button>
          <Button onClick={() => void handleSubmit()} disabled={!canSubmit}>
            {submitting ? <Loader2 className="size-3.5 animate-spin" /> : t(($) => $.add_member_dialog.add)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Inline click-to-edit role line. Renders the current role as muted text;
// click (or click the placeholder when empty) to swap in an input that
// commits on blur / Enter and cancels on Escape. Avoids opening a modal
// for what is usually a one-word change.
function RoleEditor({ value, onSave }: { value: string; onSave: (next: string) => Promise<void> }) {
  const { t } = useT("squads");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);

  useEffect(() => { if (!editing) setDraft(value); }, [value, editing]);

  const commit = async () => {
    const next = draft.trim();
    if (next === value.trim()) { setEditing(false); return; }
    setSaving(true);
    try {
      await onSave(next);
      setEditing(false);
    } catch {
      // toast handled by mutation
    } finally {
      setSaving(false);
    }
  };

  if (editing) {
    return (
      <Input
        autoFocus
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          if (isImeComposing(e)) return;
          if (e.key === "Enter") void commit();
          else if (e.key === "Escape") { setDraft(value); setEditing(false); }
        }}
        disabled={saving}
        placeholder={t(($) => $.role_editor.placeholder)}
        className="h-6 w-40 min-w-0 px-1.5 text-caption"
      />
    );
  }

  // An empty role only offers "Add role…" while the row is hovered, so a
  // roster of role-less members doesn't repeat the prompt on every line.
  return (
    <button
      type="button"
      onClick={() => setEditing(true)}
      className={cn(
        "min-w-0 truncate text-left text-caption text-muted-foreground transition-colors hover:text-foreground",
        !value &&
          "opacity-0 focus-visible:opacity-100 group-hover/member:opacity-100 [@media(pointer:coarse)]:opacity-100",
      )}
    >
      {value ? `· ${value}` : <span className="italic">{t(($) => $.role_editor.empty)}</span>}
    </button>
  );
}

// Click-to-edit description editor for the identity block. Mirrors
// agent-detail-inspector's DescriptionEditor: opens a modal with a textarea
// (enough room for multi-paragraph descriptions); the inline trigger shows
// the current value (or a placeholder) with a hover-revealed Pencil.
function SquadDescriptionEditor({
  value,
  onSave,
}: {
  value: string;
  onSave: (next: string) => Promise<void>;
}) {
  const { t } = useT("squads");
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="group -mx-1 inline-flex max-w-full items-start gap-1.5 rounded-sm px-1 text-left transition-colors hover:bg-accent/50"
      >
        {value ? (
          <span className="line-clamp-2 text-muted-foreground">{value}</span>
        ) : (
          <span className="italic text-muted-foreground">{t(($) => $.description_dialog.placeholder_empty)}</span>
        )}
        <Pencil className="mt-1 h-3 w-3 shrink-0 text-transparent transition-colors group-hover:text-muted-foreground" />
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          {open && (
            <SquadDescriptionEditorBody
              initialValue={value}
              onSave={onSave}
              onClose={() => setOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function SquadDescriptionEditorBody({
  initialValue,
  onSave,
  onClose,
}: {
  initialValue: string;
  onSave: (next: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useT("squads");
  const [draft, setDraft] = useState(initialValue);
  const [saving, setSaving] = useState(false);
  const savingRef = useRef(false);
  const dirty = draft !== initialValue;

  const commit = async () => {
    if (savingRef.current) return;
    if (!dirty) { onClose(); return; }
    savingRef.current = true;
    setSaving(true);
    try {
      await onSave(draft);
      onClose();
    } catch {
      // toast handled by parent's mutation
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.description_dialog.title)}</DialogTitle>
      </DialogHeader>
      <textarea
        autoFocus
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        placeholder={t(($) => $.description_dialog.responsibility_placeholder)}
        rows={6}
        onKeyDown={(e) => {
          if (e.key === "Escape") { onClose(); return; }
          if (e.defaultPrevented || e.repeat || isImeComposing(e)) return;
          if (shortcutMatchesEvent(getShortcut("send"), e.nativeEvent)) {
            e.preventDefault();
            void commit();
          }
        }}
        className="w-full resize-none rounded-md border bg-transparent px-3 py-2 text-body outline-none focus-visible:border-input"
      />
      <DialogFooter>
        <Button variant="ghost" size="sm" onClick={onClose} disabled={saving}>{t(($) => $.description_dialog.cancel)}</Button>
        <Button size="sm" onClick={() => void commit()} disabled={saving || !dirty}>
          {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t(($) => $.description_dialog.save)}
        </Button>
      </DialogFooter>
    </>
  );
}

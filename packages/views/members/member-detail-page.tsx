"use client";

import { ChevronRight, UserRound } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { MemberRole } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../layout/page-header";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { ActorIssuesPanel } from "../common/actor-issues-panel";
import { useT } from "../i18n";

export function MemberDetailPage({ userId }: { userId: string }) {
  const { t } = useT("members");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const { data: members = [], isLoading } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === userId) ?? null;

  if (isLoading && !member) {
    return <MemberDetailSkeleton />;
  }

  if (!member) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <MemberBreadcrumb workspaceName={workspace?.name} title={t(($) => $.detail.breadcrumb_fallback)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <UserRound className="h-8 w-8 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium">{t(($) => $.detail.not_found_title)}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.detail.not_found_description)}
            </p>
          </div>
        </div>
      </div>
    );
  }

  const initials = member.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <MemberBreadcrumb workspaceName={workspace?.name} title={member.name} />

      <div className="flex shrink-0 items-center gap-3 border-b px-6 py-4">
        <ActorAvatarBase
          name={member.name}
          initials={initials}
          avatarUrl={member.avatar_url}
          size={44}
          className="rounded-full"
        />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate text-base font-semibold">{member.name}</h1>
            <RoleBadge role={member.role} />
          </div>
          <p className="mt-0.5 truncate text-sm text-muted-foreground">
            {member.email}
          </p>
        </div>
      </div>

      <ActorIssuesPanel actorType="member" actorId={userId} />
    </div>
  );
}

function MemberBreadcrumb({
  workspaceName,
  title,
}: {
  workspaceName: string | undefined;
  title: string;
}) {
  const { t } = useT("members");
  return (
    <PageHeader className="gap-1.5">
      <WorkspaceAvatar name={workspaceName ?? "W"} size="sm" />
      <span className="text-sm text-muted-foreground">
        {workspaceName ?? t(($) => $.detail.workspace_fallback)}
      </span>
      <ChevronRight className="h-3 w-3 text-muted-foreground" />
      <span className="text-sm text-muted-foreground">
        {t(($) => $.detail.members_breadcrumb)}
      </span>
      <ChevronRight className="h-3 w-3 text-muted-foreground" />
      <span className="truncate text-sm font-medium">{title}</span>
    </PageHeader>
  );
}

function RoleBadge({ role }: { role: MemberRole }) {
  const { t } = useT("members");
  return (
    <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
      {role === "owner"
        ? t(($) => $.role.owner)
        : role === "admin"
          ? t(($) => $.role.admin)
          : t(($) => $.role.member)}
    </span>
  );
}

function MemberDetailSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="px-5">
        <Skeleton className="h-5 w-52" />
      </PageHeader>
      <div className="flex shrink-0 items-center gap-3 border-b px-6 py-4">
        <Skeleton className="h-11 w-11 rounded-full" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-56" />
        </div>
      </div>
      <div className="flex flex-1 min-h-0 gap-4 overflow-hidden p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="flex min-w-52 flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-24 w-full rounded-lg" />
            <Skeleton className="h-24 w-full rounded-lg" />
          </div>
        ))}
      </div>
    </div>
  );
}

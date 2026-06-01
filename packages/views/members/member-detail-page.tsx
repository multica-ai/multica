"use client";

import { UserRound } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { MemberRole } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { ActorIssuesPanel } from "../common/actor-issues-panel";
import { useT } from "../i18n";

export function MemberDetailPage({ userId }: { userId: string }) {
  const { t } = useT("members");
  const paths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === userId) ?? null;

  if (isLoading && !member) {
    return <MemberDetailSkeleton />;
  }

  if (!member) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <BreadcrumbHeader
          segments={[{ href: paths.members(), label: t(($) => $.detail.members_breadcrumb) }]}
          leaf={
            <span className="truncate font-medium text-foreground">
              {t(($) => $.detail.breadcrumb_fallback)}
            </span>
          }
        />
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
      <BreadcrumbHeader
        segments={[{ href: paths.members(), label: t(($) => $.detail.members_breadcrumb) }]}
        leaf={
          <span className="truncate font-medium text-foreground">
            {member.name}
          </span>
        }
      />

      <div className="flex shrink-0 items-center gap-3 border-b px-6 py-4">
        <ActorAvatarBase
          name={member.name}
          initials={initials}
          avatarUrl={resolvePublicFileUrl(member.avatar_url)}
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
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-4 w-3" />
        <Skeleton className="h-4 w-24" />
      </div>
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

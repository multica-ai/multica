"use client";

import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useStore } from "zustand";
import { ListTodo, UserRound, Users } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import type { MemberRole, MemberWithUser } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  actorIssuesViewStore,
  type ActorIssuesScope,
} from "@multica/core/issues/stores/actor-issues-view-store";
import { countAdvancedConditions } from "@multica/core/issues/stores/filter-tree";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import {
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import {
  IssueDisplayControls,
  IssueFilterMenu,
  ViewRefreshIndicator,
} from "../issues/components/issues-header";
import { useFilterChips } from "../issues/components/filter-chips-bar";
import { IssueSurface } from "../issues/surface/issue-surface";
import type { IssueSurfaceController } from "../issues/surface/use-issue-surface-controller";
import {
  CollectionFilterButton,
  CollectionPageState,
  CollectionResetButton,
} from "../layout/collection-page";
import {
  ObjectPage,
  ObjectPanelSection,
  ObjectProperty,
} from "../layout/object-page";
import { AppLink } from "../navigation";
import { useLocale, useT } from "../i18n";

const LAYOUT_ID = "multica_member_detail_layout";
const SCOPE_VALUES: ActorIssuesScope[] = ["assigned", "created"];

/**
 * A member's page is the list of issues assigned to or created by them, so it
 * keeps the list full width under the breadcrumb header; who they are sits in
 * the properties panel.
 */
export function MemberDetailPage({ userId }: { userId: string }) {
  const { t } = useT("members");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: members = [], isLoading } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === userId) ?? null;
  const segments = [
    {
      href: `${paths.settings()}?tab=members`,
      label: t(($) => $.detail.members_breadcrumb),
    },
  ];

  if (member) return <MemberIssues member={member} segments={segments} />;

  if (isLoading) {
    return (
      <ObjectPage
        layoutId={LAYOUT_ID}
        segments={segments}
        leaf={<Skeleton className="h-4 w-32" />}
        panel={<PanelSkeleton />}
      >
        <div className="space-y-2 p-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      </ObjectPage>
    );
  }

  return (
    <ObjectPage
      layoutId={LAYOUT_ID}
      segments={segments}
      leaf={
        <span className="truncate text-muted-foreground">
          {t(($) => $.detail.breadcrumb_fallback)}
        </span>
      }
    >
      <CollectionPageState
        icon={UserRound}
        title={t(($) => $.detail.not_found_title)}
        description={t(($) => $.detail.not_found_description)}
      />
    </ObjectPage>
  );
}

function MemberIssues({
  member,
  segments,
}: {
  member: MemberWithUser;
  segments: { href: string; label: string }[];
}) {
  const { t } = useT("issues");
  const scope = useStore(actorIssuesViewStore, (s) => s.scope);
  const setScope = useStore(actorIssuesViewStore, (s) => s.setScope);
  // The tools read the issue surface's view store and controller, which only
  // exist inside the surface, so they render there and portal into the header.
  const [toolsSlot, setToolsSlot] = useState<HTMLDivElement | null>(null);
  const surfaceScope = useMemo(
    () =>
      ({
        type: "actor" as const,
        actorType: "member" as const,
        actorId: member.user_id,
        relation: scope,
      }),
    [member.user_id, scope],
  );

  return (
    <ObjectPage
      layoutId={LAYOUT_ID}
      segments={segments}
      leaf={
        <span className="flex min-w-0 items-center gap-1.5">
          <ActorAvatar
            actorType="member"
            actorId={member.user_id}
            size="xs"
            profileLink={false}
          />
          <span className="truncate">{member.name}</span>
        </span>
      }
      actions={<div ref={setToolsSlot} className="flex items-center gap-1" />}
      panel={<MemberProperties member={member} />}
    >
      <div className="flex h-full min-h-0 flex-col">
        <IssueSurface
          scope={surfaceScope}
          modes={["list"]}
          batchToolbar="always"
          contentClassName="p-1"
          renderHeader={({ controller }) =>
            toolsSlot
              ? createPortal(
                  <MemberIssueTools
                    controller={controller}
                    scope={scope}
                    onScopeChange={setScope}
                  />,
                  toolsSlot,
                )
              : null
          }
          renderEmpty={() => (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-faint-foreground" />
              <p className="text-body">
                {t(($) => $.actor_issues.empty[scope].title)}
              </p>
            </div>
          )}
        />
      </div>
    </ObjectPage>
  );
}

function MemberIssueTools({
  controller,
  scope,
  onScopeChange,
}: {
  controller: IssueSurfaceController;
  scope: ActorIssuesScope;
  onScopeChange: (scope: ActorIssuesScope) => void;
}) {
  const { t } = useT("issues");
  const { t: tMembers } = useT("members");
  const advancedFilter = useViewStore((s) => s.advancedFilter);
  const clearFilters = useViewStore((s) => s.clearFilters);
  const { chips } = useFilterChips(null);
  const scopeCounts = scope !== "assigned";
  const filterCount =
    chips.filter((chip) => chip.key !== "advanced").length +
    countAdvancedConditions(advancedFilter) +
    (scopeCounts ? 1 : 0);

  return (
    <>
      <IssueFilterMenu
        align="end"
        trigger={<CollectionFilterButton count={filterCount} />}
        leadingSections={
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Users className="size-3.5" />
              <span className="flex-1">{tMembers(($) => $.detail.relation)}</span>
              <span
                className={cn(
                  "ml-3 text-caption font-medium",
                  scopeCounts ? "text-primary" : "text-muted-foreground",
                )}
              >
                {t(($) => $.actor_issues.scope[scope].label)}
              </span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-auto min-w-44">
              <DropdownMenuRadioGroup
                value={scope}
                onValueChange={(next) => onScopeChange(next as ActorIssuesScope)}
              >
                {SCOPE_VALUES.map((value) => (
                  <DropdownMenuRadioItem key={value} value={value}>
                    {t(($) => $.actor_issues.scope[value].label)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        }
        scopedIssues={controller.surfaceIssues}
        facetCountsExact={controller.facetCountsExact}
        tableFacetCounts={controller.tableFacetCounts}
        onTableFacetChange={controller.setActiveTableFacet}
      />
      {filterCount > 0 ? (
        <CollectionResetButton
          onClick={() => {
            clearFilters();
            onScopeChange("assigned");
          }}
        />
      ) : null}
      <IssueDisplayControls
        scopedIssues={controller.surfaceIssues}
        hideViewToggle
        hideFilter
        iconOnly
        facetCountsExact={controller.facetCountsExact}
        tableFacetCounts={controller.tableFacetCounts}
        onTableFacetChange={controller.setActiveTableFacet}
      />
      <ViewRefreshIndicator active={controller.isRefreshing} />
    </>
  );
}

function MemberProperties({ member }: { member: MemberWithUser }) {
  const { t } = useT("members");
  const { t: tLayout } = useT("layout");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const ownedAgents = agents.filter(
    (agent) => agent.owner_id === member.user_id && !agent.archived_at,
  );
  const joined = new Date(member.created_at);

  return (
    <div className="space-y-5">
      <ObjectPanelSection title={tLayout(($) => $.object_page.properties)}>
        <ObjectProperty label={t(($) => $.detail.role)}>
          <span className="truncate">
            <RoleLabel role={member.role} />
          </span>
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.email)}>
          <span className="truncate select-text" title={member.email}>
            {member.email}
          </span>
        </ObjectProperty>
        {Number.isFinite(joined.getTime()) && (
          <ObjectProperty label={t(($) => $.detail.joined)}>
            <span className="truncate">{joined.toLocaleDateString(locale)}</span>
          </ObjectProperty>
        )}
      </ObjectPanelSection>
      {ownedAgents.length > 0 && (
        <ObjectPanelSection
          title={t(($) => $.card.agents_section, { count: ownedAgents.length })}
        >
          {ownedAgents.map((agent) => (
            <AppLink
              key={agent.id}
              href={paths.agentDetail(agent.id)}
              className="-mx-2 flex min-h-8 items-center gap-2 rounded-md px-2 text-caption transition-colors hover:bg-accent/50"
            >
              <ActorAvatar
                actorType="agent"
                actorId={agent.id}
                size="xs"
                showStatusDot
                profileLink={false}
              />
              <span className="truncate">{agent.name}</span>
            </AppLink>
          ))}
        </ObjectPanelSection>
      )}
    </div>
  );
}

function RoleLabel({ role }: { role: MemberRole }) {
  const { t } = useT("members");
  return role === "owner"
    ? t(($) => $.role.owner)
    : role === "admin"
      ? t(($) => $.role.admin)
      : t(($) => $.role.member);
}

function PanelSkeleton() {
  return (
    <div className="space-y-3 px-2">
      <Skeleton className="h-3 w-20" />
      {Array.from({ length: 3 }).map((_, i) => (
        <Skeleton key={i} className="h-4 w-full" />
      ))}
    </div>
  );
}

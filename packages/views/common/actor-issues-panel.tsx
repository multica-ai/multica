"use client";

import { useMemo, useState } from "react";
import { useStore } from "zustand";
import { ListTodo, Search } from "lucide-react";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
} from "@multica/core/types";
import {
  actorIssuesViewStore,
  type ActorIssuesScope,
} from "@multica/core/issues/stores/actor-issues-view-store";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import {
  IssueDisplayControls,
  ViewRefreshIndicator,
} from "../issues/components/issues-header";
import { IssueSurface } from "../issues/surface/issue-surface";
import { useT } from "../i18n";

export type TaskActorType = "member" | "agent";

const SCOPE_VALUES: ActorIssuesScope[] = ["assigned", "created"];

function ActorIssuesHeader({
  issues,
  search,
  onSearchChange,
  scope,
  onScopeChange,
  isRefreshing = false,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  search: string;
  onSearchChange: (value: string) => void;
  scope: ActorIssuesScope;
  onScopeChange: (scope: ActorIssuesScope) => void;
  isRefreshing?: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const { t } = useT("issues");

  return (
    <div className="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-4">
      <div className="flex items-center gap-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t(($) => $.actor_issues.search_placeholder)}
            className="h-8 w-64 pl-8 text-sm"
          />
        </div>
        <div className="flex items-center gap-1">
          {SCOPE_VALUES.map((value) => (
            <Tooltip key={value}>
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className={
                      scope === value
                        ? "bg-accent text-accent-foreground hover:bg-accent/80"
                        : "text-muted-foreground"
                    }
                    onClick={() => onScopeChange(value)}
                  >
                    {t(($) => $.actor_issues.scope[value].label)}
                  </Button>
                }
              />
              <TooltipContent side="bottom">
                {t(($) => $.actor_issues.scope[value].description)}
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
      </div>
      <div className="flex items-center">
        <IssueDisplayControls
          scopedIssues={issues}
          hideViewToggle
          facetCountsExact={facetCountsExact}
          tableFacetCounts={tableFacetCounts}
          onTableFacetChange={onTableFacetChange}
        />
        <ViewRefreshIndicator active={isRefreshing} />
      </div>
    </div>
  );
}

export function ActorIssuesPanel({
  actorType,
  actorId,
}: {
  actorType: TaskActorType;
  actorId: string;
}) {
  const { t } = useT("issues");
  const scope = useStore(actorIssuesViewStore, (s) => s.scope);
  const setScope = useStore(actorIssuesViewStore, (s) => s.setScope);
  const [search, setSearch] = useState("");
  const surfaceScope = useMemo(
    () =>
      ({
        type: "actor" as const,
        actorType,
        actorId,
        relation: scope,
      }),
    [actorId, actorType, scope],
  );

  return (
    <IssueSurface
      scope={surfaceScope}
      modes={["list"]}
      batchToolbar="always"
      contentClassName="p-1"
      search={search}
      renderHeader={({ controller }) => (
        <ActorIssuesHeader
          issues={controller.surfaceIssues}
          search={search}
          onSearchChange={setSearch}
          scope={scope}
          onScopeChange={setScope}
          isRefreshing={controller.isRefreshing}
          facetCountsExact={controller.facetCountsExact}
          tableFacetCounts={controller.tableFacetCounts}
          onTableFacetChange={controller.setActiveTableFacet}
        />
      )}
      renderEmpty={() =>
        search.trim() === "" ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">
              {t(($) => $.actor_issues.empty[scope].title)}
            </p>
            <p className="text-xs">
              {t(($) => $.actor_issues.empty[scope].description)}
            </p>
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Search className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.actor_issues.search_empty)}</p>
          </div>
        )
      }
    />
  );
}

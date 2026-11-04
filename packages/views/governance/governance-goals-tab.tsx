"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { canGovern, goalsOptions, type Goal } from "@multica/core/governance";
import { officeFloorsOptions } from "@multica/core/office";
import { squadListOptions } from "@multica/core/workspace";
import { initiativeListOptions } from "@multica/core/pipeline";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Progress } from "@multica/ui/components/ui/progress";
import { Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { GoalConfidenceChip, GoalStatusBadge } from "./goal-badges";
import { GovernanceGoalCreateDialog } from "./governance-goal-create-dialog";
import { GovernanceGoalDetail } from "./governance-goal-detail";
import { GovernancePlaceholder } from "./governance-placeholder";
import { buildGoalTree, progressPercent, type GoalTreeSection } from "./goals-tree";

/**
 * The Goals tab (spec 4.1): the company's goals as a tree (company, floors,
 * squads, initiatives), each with its progress and confidence. The goal in the URL
 * (`goal`) opens below the tree with its key results, readings and actions.
 */
export function GovernanceGoalsTab() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const editable = canGovern(role);
  const [creating, setCreating] = useState(false);
  const { pathname, searchParams, replace } = useNavigation();
  const selected = searchParams.get("goal") ?? "";
  const goals = useQuery(goalsOptions(wsId));
  const floors = useQuery(officeFloorsOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const initiatives = useQuery(initiativeListOptions(wsId));

  const select = (id: string) => {
    const next = new URLSearchParams(searchParams);
    if (id === selected) next.delete("goal");
    else next.set("goal", id);
    const search = next.toString();
    replace(`${pathname}${search ? `?${search}` : ""}`);
  };

  if (goals.isPending) {
    return (
      <div className={cn(PAGE_GUTTER, "flex flex-col gap-3 py-4")} aria-busy="true">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    );
  }
  if (goals.isError) {
    return <p className={cn(PAGE_GUTTER, "py-4 text-destructive")}>{t(($) => $.goals.load_failed)}</p>;
  }
  const list = goals.data ?? [];
  // Owners and admins create goals here; a company with none yet still shows the button.
  const createControls = editable ? (
    <>
      <div className="flex justify-end">
        <Button type="button" size="sm" onClick={() => setCreating(true)}>
          <Plus />
          {t(($) => $.goals.create.button)}
        </Button>
      </div>
      <GovernanceGoalCreateDialog open={creating} onOpenChange={setCreating} />
    </>
  ) : null;
  if (list.length === 0) {
    return (
      <div className={cn(PAGE_GUTTER, "flex flex-col gap-4 py-4")}>
        {createControls}
        <GovernancePlaceholder />
      </div>
    );
  }

  const tree = buildGoalTree(list, {
    floors: (floors.data ?? []).map((f) => ({ id: f.id, name: f.name })),
    squads: (squads.data ?? []).map((s) => ({ id: s.id, name: s.name })),
    initiatives: (initiatives.data ?? []).map((i) => ({ id: i.id, identifier: i.identifier, title: i.title })),
  });

  return (
    <div className={cn(PAGE_GUTTER, "flex flex-col gap-6 py-4")}>
      {createControls}
      <div className="flex flex-col gap-5">
        {tree.map((section) => (
          <GoalSection
            key={`${section.scope}:${section.scopeId ?? ""}`}
            section={section}
            selected={selected}
            onSelect={select}
          />
        ))}
      </div>
      {selected ? <GovernanceGoalDetail goalId={selected} onClose={() => select(selected)} /> : null}
    </div>
  );
}

function GoalSection({
  section,
  selected,
  onSelect,
}: {
  section: GoalTreeSection;
  selected: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("governance");
  const heading =
    section.scope === "company"
      ? t(($) => $.goals.tree.company)
      : (section.name ?? t(($) => $.goals.tree.unnamed, { scope: t(($) => $.goals.scope[section.scope]) }));
  return (
    <section aria-label={heading} className="flex flex-col gap-2">
      <h2 className="text-caption font-medium text-muted-foreground">
        {section.scope === "company" ? heading : `${t(($) => $.goals.scope[section.scope])} · ${heading}`}
      </h2>
      <ul className="flex flex-col divide-y rounded-lg border">
        {section.goals.map((goal) => (
          <GoalRow key={goal.id} goal={goal} active={goal.id === selected} onSelect={onSelect} />
        ))}
      </ul>
    </section>
  );
}

function GoalRow({ goal, active, onSelect }: { goal: Goal; active: boolean; onSelect: (id: string) => void }) {
  const { t } = useT("governance");
  const percent = progressPercent(goal.progress);
  return (
    <li>
      <button
        type="button"
        aria-pressed={active}
        onClick={() => onSelect(goal.id)}
        className={cn(
          "flex w-full flex-col gap-2 px-3 py-2 text-left hover:bg-muted/50",
          active && "bg-muted",
        )}
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="font-medium">{goal.title}</span>
          <span className="flex flex-wrap items-center gap-2">
            <GoalStatusBadge status={goal.status} />
            <GoalConfidenceChip confidence={goal.confidence} overridden={!!goal.confidence_override} />
          </span>
        </div>
        <div className="flex items-center gap-3">
          {percent == null ? (
            <span className="text-caption text-muted-foreground">{t(($) => $.goals.progress_none)}</span>
          ) : (
            <>
              <Progress value={percent} className="w-48" />
              <span className="text-caption tabular-nums">{t(($) => $.goals.progress, { percent })}</span>
            </>
          )}
        </div>
      </button>
    </li>
  );
}

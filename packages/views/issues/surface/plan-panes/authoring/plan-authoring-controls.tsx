"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import {
  useReorderProjectPlanParts,
  useReorderProjectPlanPhases,
} from "@multica/core/project-plan/mutations";
import type { ProjectPlanPart, ProjectPlanPhase } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ArrowDown,
  ArrowUp,
  FilePlus2,
  Link2,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { classifyPlanWriteError } from "@multica/core/project-plan/errors";
import { useT } from "../../../../i18n";
import { usePlanAuthoring } from "./plan-authoring-context";
import { movedOrder } from "./plan-ordering";

/**
 * Inline authoring affordances for the Plan Document pane.
 *
 * Deliberately additive: the read presentation is
 * untouched, and every control here is hidden until the row is hovered or a
 * control inside it takes focus. `focus-within` matters as much as hover — an
 * affordance only reachable with a pointer is not reachable at all for a
 * keyboard user, and `opacity-0` alone would leave a focused button invisible.
 *
 * Reorder is up/down rather than drag-and-drop. That is a stated tradeoff:
 * one keyboard-operable control per direction covers the acceptance bar today,
 * where a drag surface would need its own accessibility story before it could
 * ship. `ReorderPhases`/`ReorderParts` take the complete permutation either
 * way, so swapping in a drag interaction later changes no contract.
 */

/**
 * Visibility rule for a hover/focus-revealed control cluster, parameterised by
 * which row owns it.
 *
 * Phase and part rows carry DIFFERENT group names on purpose. With one shared
 * name, a phase row is an ancestor group of every part row inside it, so
 * `:focus-within` on the phase matched every descendant cluster — tabbing to
 * one part's menu lit up every sibling part's menu at once. Naming them
 * separately keeps a part's controls scoped to that part, while still letting
 * the phase's own controls appear when focus is anywhere inside it.
 */
const REVEAL_PHASE =
  "opacity-0 transition-opacity group-hover/plan-phase:opacity-100 group-focus-within/plan-phase:opacity-100 data-[state=open]:opacity-100";
const REVEAL_PART =
  "opacity-0 transition-opacity group-hover/plan-part:opacity-100 group-focus-within/plan-part:opacity-100 data-[state=open]:opacity-100";

function useReorderToast() {
  const { t } = useT("issues");
  return (err: unknown) => {
    const classified = classifyPlanWriteError(err);
    // A reorder has no dialog to hold a notice, so a conflict has to reach the
    // reader as a conflict here — same rule as the dialogs, different surface.
    const message =
      classified.message ||
      (classified.conflict
        ? t(($) => $.plan.authoring.error.conflict)
        : t(($) => $.plan.authoring.error.unavailable));
    toast.error(message);
  };
}

/** Plan-level menu: edit details, supersede, delete. Lives beside the plan title. */
export function PlanHeaderActions() {
  const { t } = useT("issues");
  const { enabled, open } = usePlanAuthoring();
  if (!enabled) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            aria-label={t(($) => $.plan.authoring.plan_menu_label)}
            data-testid="plan-authoring-plan-menu"
          >
            <Pencil />
            {t(($) => $.plan.authoring.edit_plan_trigger)}
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuItem onClick={() => open({ kind: "edit-plan" })}>
          <Pencil />
          {t(($) => $.plan.authoring.edit_plan.title)}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => open({ kind: "supersede-plan" })}>
          <FilePlus2 />
          {t(($) => $.plan.authoring.supersede.title)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => open({ kind: "delete-plan" })}>
          <Trash2 />
          {t(($) => $.plan.authoring.delete_plan.title)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Phase-level menu: add part, rename, move, delete. */
export function PhaseActions({ phase }: { phase: ProjectPlanPhase }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { enabled, projectId, overview, open } = usePlanAuthoring();
  const reorder = useReorderProjectPlanPhases(wsId, projectId);
  const onError = useReorderToast();
  if (!enabled || !overview) return null;

  const move = (direction: "up" | "down") => {
    const ordered = movedOrder(overview.phases, phase.id, direction);
    if (!ordered) return;
    reorder.mutate(
      { planId: overview.plan.id, data: { ordered_ids: ordered } },
      { onError },
    );
  };
  const index = overview.phases.findIndex((candidate) => candidate.id === phase.id);

  return (
    <div className={`flex items-center gap-0.5 ${REVEAL_PHASE}`}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t(($) => $.plan.authoring.add_part.trigger_label, { title: phase.title })}
        onClick={() => open({ kind: "add-part", phase })}
      >
        <Plus />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.plan.authoring.phase_menu_label, { title: phase.title })}
            >
              <MoreHorizontal />
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="w-52">
          <DropdownMenuItem onClick={() => open({ kind: "edit-phase", phase })}>
            <Pencil />
            {t(($) => $.plan.authoring.rename)}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={index <= 0} onClick={() => move("up")}>
            <ArrowUp />
            {t(($) => $.plan.authoring.move_up)}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={index < 0 || index >= overview.phases.length - 1}
            onClick={() => move("down")}
          >
            <ArrowDown />
            {t(($) => $.plan.authoring.move_down)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => open({ kind: "delete-phase", phase })}>
            <Trash2 />
            {t(($) => $.plan.authoring.delete_phase.title)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

/** Part-level menu: link issues, edit, move, delete. */
export function PartActions({
  phase,
  part,
}: {
  phase: ProjectPlanPhase;
  part: ProjectPlanPart;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { enabled, projectId, overview, open } = usePlanAuthoring();
  const reorder = useReorderProjectPlanParts(wsId, projectId);
  const onError = useReorderToast();
  if (!enabled || !overview) return null;

  const move = (direction: "up" | "down") => {
    const ordered = movedOrder(phase.parts, part.id, direction);
    if (!ordered) return;
    reorder.mutate(
      { planId: overview.plan.id, phaseId: phase.id, data: { ordered_ids: ordered } },
      { onError },
    );
  };
  const index = phase.parts.findIndex((candidate) => candidate.id === part.id);

  return (
    <div className={`flex items-center gap-0.5 ${REVEAL_PART}`}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t(($) => $.plan.authoring.link_issues.title)}
        onClick={() => open({ kind: "link-issues", phase, part })}
      >
        <Link2 />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.plan.authoring.part_menu_label, { title: part.title })}
            >
              <MoreHorizontal />
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="w-52">
          <DropdownMenuItem onClick={() => open({ kind: "edit-part", phase, part })}>
            <Pencil />
            {t(($) => $.plan.authoring.edit_part.title)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => open({ kind: "link-issues", phase, part })}>
            <Link2 />
            {t(($) => $.plan.authoring.link_issues.title)}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={index <= 0} onClick={() => move("up")}>
            <ArrowUp />
            {t(($) => $.plan.authoring.move_up)}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={index < 0 || index >= phase.parts.length - 1}
            onClick={() => move("down")}
          >
            <ArrowDown />
            {t(($) => $.plan.authoring.move_down)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={() => open({ kind: "delete-part", phase, part })}>
            <Trash2 />
            {t(($) => $.plan.authoring.delete_part.title)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

/** Trailing "Add phase" affordance, below the last phase in the document. */
export function AddPhaseButton() {
  const { t } = useT("issues");
  const { enabled, open } = usePlanAuthoring();
  if (!enabled) return null;
  return (
    <Button
      variant="outline"
      size="sm"
      className="w-full border-dashed"
      data-testid="plan-authoring-add-phase"
      onClick={() => open({ kind: "add-phase" })}
    >
      <Plus />
      {t(($) => $.plan.authoring.add_phase.submit)}
    </Button>
  );
}

/**
 * Empty-state entry point: create a plan by hand on a project that has none.
 * Rendered inside the Phase 1 no-plan state, which is where a person actually
 * discovers that a plan is missing.
 */
export function CreatePlanButton() {
  const { t } = useT("issues");
  const { enabled, open } = usePlanAuthoring();
  if (!enabled) return null;
  return (
    <Button
      size="sm"
      className="mt-1"
      data-testid="plan-authoring-create-plan"
      onClick={() => open({ kind: "create-plan" })}
    >
      <Plus />
      {t(($) => $.plan.authoring.create_plan.submit)}
    </Button>
  );
}

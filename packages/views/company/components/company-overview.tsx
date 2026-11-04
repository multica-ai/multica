"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  companyFloorsOptions,
  useDeleteCompanyFloor,
  useSetCompanySeats,
  useUpdateCompanyFloor,
  type CompanyFloor,
} from "@multica/core/company";
import { agentListOptions, squadListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Squad } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { ArrowDown, ArrowUp, ChevronRight, MoreHorizontal, Pencil, Plus, Trash2 } from "@multica/ui/icons";
import { CreateSquadModal } from "../../modals/create-squad";
import { useIssuePeekActions } from "../../issues/surface/peek-context";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { useCompanyAdmin } from "../lib/use-company-admin";
import { firstFreeSlot } from "../lib/company-seats";
import {
  UNSEATED_DEPARTMENT,
  companyDepth,
  companySelectionQuery,
  type CompanySelection,
} from "../lib/company-selection";
import { CompanySquadDetail } from "./company-squad-detail";
import { CompanyAgentDetail } from "./company-agent-detail";
import { FloorNameDialog } from "./company-floor-dialogs";

interface Department {
  id: string;
  name: string;
  squads: Squad[];
}

/**
 * The company on one page: departments (floors) on the left, a department's
 * squads in the middle, and the open squad or agent on the right. Each level
 * is a query param, so the page never changes route while drilling down.
 * Below `md` one level shows at a time and the breadcrumb steps back.
 */
export function CompanyOverview({ selection }: { selection: CompanySelection }) {
  const { t } = useT("company");
  const { t: tc } = useT("common");
  const wsId = useWorkspaceId();
  const isAdmin = useCompanyAdmin();
  const peek = useIssuePeekActions();
  const queryClient = useQueryClient();
  const { pathname, push } = useNavigation();
  const { data: floors = [] } = useQuery(companyFloorsOptions(wsId));
  const { data: squadsAll = [] } = useQuery(squadListOptions(wsId));
  const { data: agentsAll = [] } = useQuery(agentListOptions(wsId));
  const updateFloor = useUpdateCompanyFloor();
  const deleteFloor = useDeleteCompanyFloor();
  const setSeats = useSetCompanySeats();

  const [editingFloor, setEditingFloor] = useState<CompanyFloor | "new" | null>(null);
  const [floorToDelete, setFloorToDelete] = useState<CompanyFloor | null>(null);
  const [creatingSquad, setCreatingSquad] = useState(false);

  const squads = useMemo(() => squadsAll.filter((squad) => !squad.archived_at), [squadsAll]);
  const agents = useMemo(() => agentsAll.filter((agent) => !agent.archived_at), [agentsAll]);
  const sortedFloors = useMemo(() => [...floors].sort((a, b) => a.position - b.position), [floors]);

  const departments = useMemo<Department[]>(() => {
    const list: Department[] = sortedFloors.map((floor) => ({
      id: floor.id,
      name: floor.name,
      squads: squads.filter((squad) => squad.floor_id === floor.id),
    }));
    const unseated = squads.filter((squad) => !squad.floor_id);
    if (unseated.length > 0) {
      list.push({ id: UNSEATED_DEPARTMENT, name: t(($) => $.unseated), squads: unseated });
    }
    return list;
  }, [sortedFloors, squads, t]);

  // A squad drill-down implies its department even when the URL names only the squad.
  const openSquad = squads.find((squad) => squad.id === selection.squad) ?? null;
  const departmentId =
    selection.department ?? (openSquad ? (openSquad.floor_id ?? UNSEATED_DEPARTMENT) : null);
  const department = departments.find((d) => d.id === departmentId) ?? null;
  const openAgent = selection.squad ? (agents.find((agent) => agent.id === selection.agent) ?? null) : null;
  const depth = companyDepth({ ...selection, department: departmentId });

  const go = (next: Partial<CompanySelection>) =>
    push(`${pathname}${companySelectionQuery({ ...selection, ...next })}`);
  const selectDepartment = (id: string) => go({ department: id, squad: null, agent: null });
  const selectSquad = (squad: Squad) =>
    go({ department: squad.floor_id ?? UNSEATED_DEPARTMENT, squad: squad.id, agent: null });
  const selectAgent = (id: string) => go({ agent: id });

  const crumbs: { label: string; onClick?: () => void }[] = [
    { label: t(($) => $.title), onClick: depth > 0 ? () => go({ department: null, squad: null, agent: null }) : undefined },
  ];
  if (department) {
    crumbs.push({
      label: department.name,
      onClick: depth > 1 ? () => go({ squad: null, agent: null }) : undefined,
    });
  }
  if (openSquad) {
    crumbs.push({ label: openSquad.name, onClick: openAgent ? () => go({ agent: null }) : undefined });
  }
  if (openAgent) crumbs.push({ label: openAgent.name });

  const detailShown = depth >= 2;
  const shows = (level: number) => depth === level;

  const moveFloor = (floor: CompanyFloor, direction: -1 | 1) => {
    const index = sortedFloors.findIndex((f) => f.id === floor.id);
    const target = sortedFloors[index + direction];
    if (!target) return;
    updateFloor.mutate(
      { id: floor.id, position: target.position },
      { onError: () => toast.error(t(($) => $.department_move_failed)) },
    );
  };

  const removeFloor = (floor: CompanyFloor) => {
    deleteFloor.mutate(floor.id, {
      onSuccess: () => {
        setFloorToDelete(null);
        if (departmentId === floor.id) go({ department: null, squad: null, agent: null });
      },
      onError: () => toast.error(t(($) => $.department_delete_failed)),
    });
  };

  // A new squad joins the department it was made in when that department has a free room.
  const handleSquadCreated = async (squad: Squad) => {
    const target = isAdmin && departmentId && departmentId !== UNSEATED_DEPARTMENT ? departmentId : null;
    if (target) {
      const slot = firstFreeSlot(squadsAll, target);
      if (slot === null) {
        toast.error(t(($) => $.department_full));
      } else {
        try {
          await setSeats.mutateAsync([{ squad_id: squad.id, floor_id: target, slot }]);
        } catch {
          toast.error(t(($) => $.squad_move_failed));
        }
      }
    }
    queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
    go({ department: target ?? UNSEATED_DEPARTMENT, squad: squad.id, agent: null });
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {depth > 0 ? (
        <nav aria-label={t(($) => $.breadcrumb)} className="flex min-w-0 items-center gap-1 border-b px-4 py-2 text-sm">
          {crumbs.map((crumb, index) => (
            <span key={index} className="flex min-w-0 items-center gap-1">
              {index > 0 ? <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" /> : null}
              {crumb.onClick ? (
                <button type="button" onClick={crumb.onClick} className="truncate text-muted-foreground hover:text-foreground">
                  {crumb.label}
                </button>
              ) : (
                <span className="truncate font-medium">{crumb.label}</span>
              )}
            </span>
          ))}
        </nav>
      ) : null}

      <div className="flex min-h-0 flex-1">
        <aside
          aria-label={t(($) => $.departments)}
          className={cn("w-60 shrink-0 flex-col overflow-y-auto border-r p-2", depth === 0 ? "flex" : "hidden md:flex")}
        >
          <div className="flex items-center justify-between px-2 pb-2 pt-1">
            <h2 className="text-xs font-medium text-muted-foreground">{t(($) => $.departments)}</h2>
            {isAdmin ? (
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={t(($) => $.department_add)}
                onClick={() => setEditingFloor("new")}
              >
                <Plus />
              </Button>
            ) : null}
          </div>
          {departments.map((d) => {
            const floor = sortedFloors.find((f) => f.id === d.id) ?? null;
            const index = floor ? sortedFloors.indexOf(floor) : -1;
            return (
              <div key={d.id} className="group/dept flex items-center gap-1">
                <button
                  type="button"
                  aria-current={departmentId === d.id ? "true" : undefined}
                  onClick={() => selectDepartment(d.id)}
                  className={cn(
                    "flex min-w-0 flex-1 flex-col rounded-md px-2 py-2 text-left hover:bg-accent/60",
                    departmentId === d.id && "bg-accent font-medium",
                  )}
                >
                  <span className="truncate text-sm">{d.name}</span>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {t(($) => $.summary_squads, { count: d.squads.length })}
                  </span>
                </button>
                {isAdmin && floor ? (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          aria-label={t(($) => $.department_menu, { name: floor.name })}
                          className="opacity-0 group-hover/dept:opacity-100 data-[popup-open]:opacity-100"
                        />
                      }
                    >
                      <MoreHorizontal />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start" className="min-w-44">
                      <DropdownMenuItem onClick={() => setEditingFloor(floor)}>
                        <Pencil />
                        {t(($) => $.department_rename)}
                      </DropdownMenuItem>
                      <DropdownMenuItem disabled={index <= 0} onClick={() => moveFloor(floor, -1)}>
                        <ArrowUp />
                        {t(($) => $.department_move_up)}
                      </DropdownMenuItem>
                      <DropdownMenuItem disabled={index >= sortedFloors.length - 1} onClick={() => moveFloor(floor, 1)}>
                        <ArrowDown />
                        {t(($) => $.department_move_down)}
                      </DropdownMenuItem>
                      <DropdownMenuItem variant="destructive" onClick={() => setFloorToDelete(floor)}>
                        <Trash2 />
                        {t(($) => $.department_delete)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                ) : null}
              </div>
            );
          })}
        </aside>

        <section
          aria-label={t(($) => $.squads)}
          className={cn("w-72 shrink-0 flex-col overflow-y-auto border-r p-2", shows(1) ? "flex" : "hidden md:flex")}
        >
          <div className="flex items-center justify-between px-2 pb-2 pt-1">
            <h2 className="text-xs font-medium text-muted-foreground">{t(($) => $.squads)}</h2>
            <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.squad_add)} onClick={() => setCreatingSquad(true)}>
              <Plus />
            </Button>
          </div>
          {department && department.squads.length === 0 ? (
            <p className="px-2 text-sm text-muted-foreground">{t(($) => $.no_squads)}</p>
          ) : null}
          {department?.squads.map((squad) => (
            <button
              key={squad.id}
              type="button"
              aria-current={openSquad?.id === squad.id ? "true" : undefined}
              onClick={() => selectSquad(squad)}
              className={cn(
                "flex min-w-0 flex-col rounded-md px-2 py-2 text-left hover:bg-accent/60",
                openSquad?.id === squad.id && "bg-accent font-medium",
              )}
            >
              <span className="truncate text-sm">{squad.name}</span>
              <span className="truncate text-xs text-muted-foreground">
                {t(($) => $.member_count, { count: squad.member_count ?? 0 })}
              </span>
            </button>
          ))}
        </section>

        <main
          className={cn(
            "min-w-0 flex-1 overflow-y-auto p-5",
            detailShown ? "flex flex-col" : "hidden md:flex md:flex-col",
          )}
        >
          {openSquad && openAgent ? (
            <CompanyAgentDetail
              wsId={wsId}
              agentId={openAgent.id}
              onArchived={() => go({ agent: null })}
            />
          ) : openSquad ? (
            <CompanySquadDetail
              wsId={wsId}
              squad={openSquad}
              floors={sortedFloors}
              squads={squadsAll}
              departmentId={departmentId ?? UNSEATED_DEPARTMENT}
              canSeat={isAdmin}
              onOpenAgent={selectAgent}
              onOpenIssue={(issueId) => peek?.open(issueId)}
              onDeleted={() => go({ squad: null, agent: null })}
              onMoved={(next) => go({ department: next })}
            />
          ) : (
            <Summary departments={departments} squads={squads} agents={agents} department={department} />
          )}
        </main>
      </div>

      {editingFloor ? (
        <FloorNameDialog
          floor={editingFloor === "new" ? null : editingFloor}
          onClose={() => setEditingFloor(null)}
          onSaved={(saved) => {
            if (editingFloor === "new") selectDepartment(saved.id);
          }}
        />
      ) : null}

      {creatingSquad ? (
        <CreateSquadModal onClose={() => setCreatingSquad(false)} onCreated={handleSquadCreated} />
      ) : null}

      <AlertDialog open={!!floorToDelete} onOpenChange={(open) => !open && setFloorToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.department_delete_title, { name: floorToDelete?.name ?? "" })}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.department_delete_body)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tc(($) => $.cancel)}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => floorToDelete && removeFloor(floorToDelete)}>
              {t(($) => $.department_delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Summary({
  departments,
  squads,
  agents,
  department,
}: {
  departments: Department[];
  squads: Squad[];
  agents: { id: string }[];
  department: Department | null;
}) {
  const { t } = useT("company");
  const stats = department
    ? [
        { label: t(($) => $.squads), value: department.squads.length },
        {
          label: t(($) => $.members),
          value: department.squads.reduce((sum, squad) => sum + (squad.member_count ?? 0), 0),
        },
      ]
    : [
        { label: t(($) => $.departments), value: departments.filter((d) => d.id !== UNSEATED_DEPARTMENT).length },
        { label: t(($) => $.squads), value: squads.length },
        { label: t(($) => $.agents), value: agents.length },
      ];
  return (
    <div className="flex flex-col gap-4">
      {department ? <h2 className="text-lg font-semibold">{department.name}</h2> : null}
      <dl className="grid grid-cols-2 gap-3 md:grid-cols-3">
        {stats.map((stat) => (
          <div key={stat.label} className="flex flex-col gap-1 rounded-lg border p-4">
            <dt className="text-xs text-muted-foreground">{stat.label}</dt>
            <dd className="text-2xl font-semibold tabular-nums">{stat.value}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

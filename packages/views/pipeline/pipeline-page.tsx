"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  canManageInitiatives,
  initiativeListOptions,
  pipelineListOptions,
} from "@multica/core/pipeline";
import { squadListOptions } from "@multica/core/workspace/queries";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Button } from "@multica/ui/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { Plus, Workflow } from "@multica/ui/icons";
import { CollectionPageHeader } from "../layout/collection-page";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { InitiativeCreateDialog } from "./initiative-create-dialog";
import { InitiativePanel } from "./initiative-panel";
import { PipelineBoard } from "./pipeline-board";
import {
  STATUS_FILTERS,
  matchesStatusFilter,
  parseStatusFilter,
  stageColumns,
  statusParamFor,
  type StatusFilter,
} from "./pipeline-filters";

const ALL_SQUADS = "";

/**
 * The pipeline: one board per workspace, a column per stage of the default
 * pipeline. The owner squad and the status are read from the URL, so a link
 * from an Office room opens the board filtered to that squad. Selecting a card
 * opens its initiative in a side panel.
 */
export function PipelinePage() {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const { pathname, searchParams, replace } = useNavigation();
  const squadFilter = searchParams.get("squad") ?? ALL_SQUADS;
  const statusFilter = parseStatusFilter(searchParams.get("status"));
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const { role } = useCurrentMember(wsId);
  const canManage = canManageInitiatives(role);

  const pipelines = useQuery(pipelineListOptions(wsId));
  const squads = useQuery(squadListOptions(wsId));
  const pipeline = pipelines.data?.find((candidate) => candidate.is_default) ?? pipelines.data?.[0] ?? null;
  const initiatives = useQuery({
    ...initiativeListOptions(wsId, {
      pipeline_id: pipeline?.id,
      owner_squad_id: squadFilter || undefined,
      status: statusParamFor(statusFilter),
    }),
    enabled: !!wsId && !!pipeline,
  });

  const visible = useMemo(
    () => (initiatives.data ?? []).filter((initiative) => matchesStatusFilter(initiative, statusFilter)),
    [initiatives.data, statusFilter],
  );
  const columns = useMemo(() => stageColumns(pipeline?.stages ?? [], visible), [pipeline, visible]);
  const squadList = squads.data ?? [];
  const selected = (initiatives.data ?? []).find((initiative) => initiative.id === selectedId) ?? null;

  // Filters live in the URL so a filtered board can be linked and survives a reload.
  const setParam = (name: "squad" | "status", value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(name, value);
    else next.delete(name);
    const search = next.toString();
    replace(`${pathname}${search ? `?${search}` : ""}`);
  };

  const squadItems = [
    { value: ALL_SQUADS, label: t(($) => $.toolbar.squad_all) },
    ...squadList.filter((squad) => !squad.archived_at).map((squad) => ({ value: squad.id, label: squad.name })),
  ];
  const statusItems = STATUS_FILTERS.map((filter: StatusFilter) => ({
    value: filter,
    label: t(($) => $.toolbar.status_option[filter]),
  }));

  const tools = (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <Select
        items={squadItems}
        value={squadFilter}
        onValueChange={(value) => setParam("squad", value && value !== ALL_SQUADS ? value : ALL_SQUADS)}
      >
        <SelectTrigger aria-label={t(($) => $.toolbar.squad)} className="w-44">
          <SelectValue>
            {squadFilter
              ? (squadList.find((squad) => squad.id === squadFilter)?.name ?? t(($) => $.toolbar.squad))
              : t(($) => $.toolbar.squad_all)}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_SQUADS}>{t(($) => $.toolbar.squad_all)}</SelectItem>
          {squadItems.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        items={statusItems}
        value={statusFilter}
        onValueChange={(value) => setParam("status", value === "open" ? "" : (value ?? ""))}
      >
        <SelectTrigger aria-label={t(($) => $.toolbar.status)} className="w-36">
          <SelectValue>{t(($) => $.toolbar.status_option[statusFilter])}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {statusItems.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Workflow}
        title={t(($) => $.title)}
        tools={tools}
        actions={
          canManage && pipeline ? (
            <Button type="button" size="sm" onClick={() => setCreating(true)}>
              <Plus />
              {t(($) => $.create.button)}
            </Button>
          ) : undefined
        }
      />
      {canManage ? <InitiativeCreateDialog open={creating} onOpenChange={setCreating} /> : null}
      {pipelines.isPending ? null : !pipeline ? (
        <p className="px-4 py-8 text-center text-body text-muted-foreground">{t(($) => $.board.no_pipeline)}</p>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-3 pt-3">
          {initiatives.isError ? (
            <p role="alert" className="px-4 text-caption text-destructive">
              {t(($) => $.board.load_failed)}
            </p>
          ) : initiatives.isSuccess && visible.length === 0 ? (
            <p className="px-4 text-caption text-muted-foreground">{t(($) => $.board.no_initiatives)}</p>
          ) : null}
          <PipelineBoard
            columns={columns}
            squads={squadList}
            selectedId={selectedId}
            onSelect={setSelectedId}
          />
        </div>
      )}
      <Sheet open={selectedId !== null} onOpenChange={(open) => !open && setSelectedId(null)}>
        <SheetContent className="w-full overflow-y-auto sm:max-w-md">
          <SheetHeader>
            <SheetTitle className="truncate pr-8">
              {selected ? `${selected.identifier} ${selected.title}` : t(($) => $.panel.loading)}
            </SheetTitle>
          </SheetHeader>
          {selectedId ? <InitiativePanel initiativeId={selectedId} pipeline={pipeline} squads={squadList} /> : null}
        </SheetContent>
      </Sheet>
    </div>
  );
}

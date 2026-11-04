"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, errorCode } from "@multica/core/api";
import {
  parseSquadProjectMapping,
  useAssignSquadProject,
  type SquadProjectStatusMapping,
} from "@multica/core/company";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project, Squad } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { squadListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Search } from "@multica/ui/icons";
import { useT } from "../../i18n";

/** The statuses the SDLC Auto workflow lists, in the order its steps run. */
const SDLC_STATUS_KEYS = ["todo", "in_review", "qa"] as const;

/** Projects that are finished cannot take new work. */
const CLOSED_PROJECT_STATUSES = new Set(["completed", "cancelled"]);

/**
 * Picks a project for a squad's room. The project takes the SDLC Auto workflow,
 * and the squad's previous project is unassigned. When the project's issues sit on
 * statuses the workflow lacks, the dialog asks where each group moves first.
 */
export function CompanyAssignProjectDialog({
  squad,
  onClose,
  onAssigned,
  onOpenSquad,
}: {
  /** The squad to assign to; the dialog is closed while this is null. */
  squad: Squad | null;
  onClose: () => void;
  /** Called once the server has assigned the project. */
  onAssigned: (project: Project) => void;
  /** Opens the squad's room panel. */
  onOpenSquad: () => void;
}) {
  return (
    <Dialog open={squad !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,36rem)] flex-col gap-0 p-0 sm:max-w-md">
        {squad ? (
          <AssignBody key={squad.id} squad={squad} onAssigned={onAssigned} onOpenSquad={onOpenSquad} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function AssignBody({
  squad,
  onAssigned,
  onOpenSquad,
}: {
  squad: Squad;
  onAssigned: (project: Project) => void;
  onOpenSquad: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const assign = useAssignSquadProject(squad.id);

  const [query, setQuery] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Set after the server asks where issues on unsupported statuses move.
  const [mapping, setMapping] = useState<SquadProjectStatusMapping[] | null>(null);
  const [statusMapping, setStatusMapping] = useState<Record<string, string>>({});

  const squadName = useMemo(() => new Map(squads.map((s) => [s.id, s.name])), [squads]);
  const candidates = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return projects
      .filter((project) => !CLOSED_PROJECT_STATUSES.has(project.status))
      .filter((project) => !needle || project.title.toLowerCase().includes(needle))
      .sort((a, b) => Number(b.squad_id === squad.id) - Number(a.squad_id === squad.id));
  }, [projects, query, squad.id]);

  const submit = () => {
    if (!selectedId) return;
    const project = projects.find((p) => p.id === selectedId);
    if (!project) return;
    assign.mutate(
      { project_id: selectedId, status_mapping: mapping ? statusMapping : undefined },
      {
        onSuccess: () => {
          toast.success(t(($) => $.company.assign_done, { project: project.title, name: squad.name }));
          onAssigned(project);
        },
        onError: (error) => {
          const required = error instanceof ApiError ? parseSquadProjectMapping(error.body) : null;
          if (required) {
            // Suggest each status's target so the common case is one click.
            setMapping(required);
            setStatusMapping(Object.fromEntries(required.map((row) => [row.status_key, row.suggested_status_key])));
            return;
          }
          const code = errorCode(error);
          toast.error(
            code === "build_squad_not_sdlc"
              ? t(($) => $.company.assign_not_sdlc)
              : code === "project_workflow_set"
                ? t(($) => $.company.assign_workflow_set)
                : t(($) => $.company.assign_failed),
          );
        },
      },
    );
  };

  const mappingReady = !mapping || mapping.every((row) => SDLC_STATUS_KEYS.includes(statusMapping[row.status_key] as (typeof SDLC_STATUS_KEYS)[number]));

  return (
    <>
      <DialogHeader className="gap-1 border-b px-4 py-3 pr-12">
        <DialogTitle className="text-heading">{t(($) => $.company.assign_title, { name: squad.name })}</DialogTitle>
        <p className="text-body text-muted-foreground">{t(($) => $.company.assign_description)}</p>
      </DialogHeader>

      {mapping ? (
        <div className="flex flex-col gap-3 overflow-y-auto px-4 py-3">
          <p className="text-body">{t(($) => $.company.assign_mapping_intro)}</p>
          {mapping.map((row) => (
            <div key={row.status_key} className="flex items-center justify-between gap-3">
              <span className="text-body">
                {t(($) => $.company.assign_mapping_row, { count: row.issue_count, status: row.status_key })}
              </span>
              <Select
                items={SDLC_STATUS_KEYS.map((key) => ({ value: key, label: key }))}
                value={statusMapping[row.status_key] ?? ""}
                onValueChange={(value) => setStatusMapping((prev) => ({ ...prev, [row.status_key]: value ?? "" }))}
              >
                <SelectTrigger aria-label={row.status_key} className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SDLC_STATUS_KEYS.map((key) => (
                    <SelectItem key={key} value={key}>
                      {key}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-2 px-4 py-3">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.company.assign_search)}
              aria-label={t(($) => $.company.assign_search)}
              className="pl-8"
            />
          </div>
          <ul className="flex min-h-0 flex-col gap-1 overflow-y-auto" aria-label={t(($) => $.company.assign_list)}>
            {candidates.length === 0 ? (
              <li className="px-2 py-6 text-center text-body text-muted-foreground">{t(($) => $.company.assign_empty)}</li>
            ) : null}
            {candidates.map((project) => {
              const selected = project.id === selectedId;
              const elsewhere = project.squad_id && project.squad_id !== squad.id ? squadName.get(project.squad_id) : null;
              return (
                <li key={project.id}>
                  <button
                    type="button"
                    aria-pressed={selected}
                    onClick={() => setSelectedId(project.id)}
                    className={`flex w-full items-center justify-between gap-3 rounded-md border px-3 py-2 text-left text-body ${
                      selected ? "border-[#1d5fd6] bg-[#1d5fd6]/10" : "hover:bg-accent"
                    }`}
                  >
                    <span className="truncate">{project.title}</span>
                    {project.squad_id === squad.id ? (
                      <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.company.assign_current)}</span>
                    ) : elsewhere ? (
                      <span className="shrink-0 text-caption text-muted-foreground">
                        {t(($) => $.company.assign_elsewhere, { name: elsewhere })}
                      </span>
                    ) : null}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <DialogFooter className="flex-row items-center justify-between gap-2 border-t px-4 py-3">
        <Button type="button" variant="ghost" size="sm" onClick={onOpenSquad}>
          {t(($) => $.company.assign_see_squad)}
        </Button>
        <div className="flex gap-2">
          {mapping ? (
            <Button type="button" variant="outline" size="sm" onClick={() => setMapping(null)} disabled={assign.isPending}>
              {t(($) => $.company.assign_back)}
            </Button>
          ) : null}
          <Button
            type="button"
            size="sm"
            disabled={!selectedId || assign.isPending || !mappingReady}
            aria-busy={assign.isPending}
            onClick={submit}
          >
            {mapping ? t(($) => $.company.assign_submit_mapped) : t(($) => $.company.assign_submit)}
          </Button>
        </div>
      </DialogFooter>
    </>
  );
}

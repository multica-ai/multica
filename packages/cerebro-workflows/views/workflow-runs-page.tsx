"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { Button } from "@multica/ui/components/ui/button";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import {
  RUN_STATUS_BADGE,
  cerebroWorkflowRunsOptions,
  cerebroWorkflowsListOptions,
} from "../core";
import type { CerebroWorkflowRun, CerebroWorkflowRunStatus } from "../core/types";

const PAGE_SIZE = 50;

interface WorkflowRunsPageProps {
  workflowId?: string;
}

// Workflow-log page (JEH-1047). Filters: workflow + status. When workflowId
// is supplied (per-workflow route) the workflow filter is locked.
export function WorkflowRunsPage({ workflowId }: WorkflowRunsPageProps) {
  const enabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";

  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(workflowId ?? null);
  const [statusFilter, setStatusFilter] = useState<CerebroWorkflowRunStatus | "">("");
  const [offset, setOffset] = useState(0);

  const workflows = useQuery(cerebroWorkflowsListOptions(wsId));
  const runs = useQuery(cerebroWorkflowRunsOptions(wsId, selectedWorkflowId, PAGE_SIZE, offset));

  if (!enabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  // Status filter is applied client-side because the count is bounded by
  // PAGE_SIZE per fetch. Pushing it to the server is a phase-2 optimisation
  // once the table grows past a few hundred rows.
  const rows: CerebroWorkflowRun[] = (runs.data?.runs ?? []).filter((r) =>
    statusFilter === "" ? true : r.status === statusFilter,
  );
  const workflowMap = new Map((workflows.data?.workflows ?? []).map((w) => [w.id, w.name]));

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Workflow-log</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Eksekveringer på tværs af workflows
          </p>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-4 p-6">
          <div className="flex flex-wrap items-end gap-3">
            {!workflowId && (
              <FilterField label="Workflow">
                <NativeSelect
                  value={selectedWorkflowId ?? ""}
                  onChange={(e) => {
                    setSelectedWorkflowId(e.target.value || null);
                    setOffset(0);
                  }}
                >
                  <option value="">Alle</option>
                  {(workflows.data?.workflows ?? []).map((w) => (
                    <option key={w.id} value={w.id}>
                      {w.name}
                    </option>
                  ))}
                </NativeSelect>
              </FilterField>
            )}
            <FilterField label="Status">
              <NativeSelect
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as CerebroWorkflowRunStatus | "")}
              >
                <option value="">Alle</option>
                <option value="queued">Queued</option>
                <option value="running">Running</option>
                <option value="success">Success</option>
                <option value="failed">Failed</option>
                <option value="escalated">Escalated</option>
              </NativeSelect>
            </FilterField>
          </div>

          {runs.isError && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              Kunne ikke hente runs.
            </div>
          )}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[22%]">Tidspunkt</TableHead>
                <TableHead className="w-[22%]">Workflow</TableHead>
                <TableHead className="w-[14%]">Status</TableHead>
                <TableHead className="w-[10%]">Forsøg</TableHead>
                <TableHead>Note</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 && !runs.isLoading && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                    Ingen kørsler matcher filtrene.
                  </TableCell>
                </TableRow>
              )}
              {rows.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="text-xs">
                    {new Date(r.created_at).toLocaleString()}
                  </TableCell>
                  <TableCell className="text-xs">
                    {workflowMap.get(r.workflow_id) ?? r.workflow_id}
                  </TableCell>
                  <TableCell>
                    <span
                      className={`inline-block rounded px-2 py-0.5 text-[11px] font-medium ${RUN_STATUS_BADGE[r.status]}`}
                    >
                      {r.status}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs">{r.attempt}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {r.error ?? (r.next_retry_at
                      ? `Næste forsøg: ${new Date(r.next_retry_at).toLocaleString()}`
                      : "")}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>Side {Math.floor(offset / PAGE_SIZE) + 1}</span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                Forrige
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={(runs.data?.runs.length ?? 0) < PAGE_SIZE}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                Næste
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] uppercase tracking-wide text-muted-foreground">{label}</span>
      {children}
    </div>
  );
}

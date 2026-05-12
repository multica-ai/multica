"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import {
  cerebroWorkflowsKeys,
  cerebroWorkflowsListOptions,
  deleteWorkflow,
  toggleWorkflow,
} from "../core";
import type { CerebroWorkflow } from "../core/types";

// List page for the cerebro workflow engine (JEH-1047). Shows every workflow
// in the workspace plus a quick enable/disable toggle. Hidden when the
// `cerebro_workflows` feature flag is off — and rows still won't execute on
// the server unless CEREBRO_WORKFLOWS_ENABLED is set there.
export function WorkflowsPage() {
  const enabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();

  const wsId = workspace?.id ?? "";
  const list = useQuery(cerebroWorkflowsListOptions(wsId));

  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => toggleWorkflow(id, enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.list(wsId) });
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => deleteWorkflow(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.list(wsId) });
    },
  });

  if (!enabled) return null;

  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const workflows: CerebroWorkflow[] = list.data?.workflows ?? [];

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">Workflows</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Data-drevne regler der reagerer på issue-events
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            onClick={() => navigation.push(`/${workspace.slug}/workflows/new`)}
          >
            <Plus className="size-4" />
            Nyt workflow
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => navigation.push(`/${workspace.slug}/workflows/runs`)}
          >
            Workflow-log
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-4 p-6">
          {list.isError && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              Kunne ikke hente workflows. {list.error instanceof Error ? list.error.message : ""}
            </div>
          )}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[28%]">Navn</TableHead>
                <TableHead className="w-[22%]">Trigger</TableHead>
                <TableHead className="w-[22%]">Action</TableHead>
                <TableHead className="w-[12%]">Aktiv</TableHead>
                <TableHead className="w-[16%] text-right">Handlinger</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {workflows.length === 0 && !list.isLoading && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                    Ingen workflows endnu. Tryk på <strong>Nyt workflow</strong> for at oprette en regel.
                  </TableCell>
                </TableRow>
              )}
              {workflows.map((wf) => (
                <TableRow key={wf.id}>
                  <TableCell>
                    <button
                      type="button"
                      className="text-left hover:underline"
                      onClick={() => navigation.push(`/${workspace.slug}/workflows/${wf.id}`)}
                    >
                      <div className="font-medium">{wf.name}</div>
                      <div className="text-[11px] text-muted-foreground">
                        Oprettet {new Date(wf.created_at).toLocaleString()}
                      </div>
                    </button>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{wf.trigger_type}</TableCell>
                  <TableCell className="font-mono text-xs">{wf.action_type}</TableCell>
                  <TableCell>
                    <Switch
                      checked={wf.enabled}
                      onCheckedChange={(v) => toggle.mutate({ id: wf.id, enabled: v === true })}
                      disabled={toggle.isPending}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => navigation.push(`/${workspace.slug}/workflows/${wf.id}/runs`)}
                    >
                      Runs
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive"
                      onClick={() => {
                        if (confirm(`Slet workflow "${wf.name}"?`)) remove.mutate(wf.id);
                      }}
                    >
                      Slet
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>
    </div>
  );
}

"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Menu, Plus } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
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
  const evalsEnabled = useFeatureFlag("cerebro_evals");
  const hooksEnabled = useFeatureFlag("cerebro_workflow_hooks");
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
        Loading workspace context…
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
            Data-driven rules that react to issue events
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="hidden items-center gap-2 md:flex">{evalsEnabled && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => navigation.push(`/${workspace.slug}/workflows/evals`)}
            >
              Eval catalog
            </Button>
          )}
          {hooksEnabled && <Button
            size="sm"
            variant="outline"
            onClick={() => navigation.push(`/${workspace.slug}/workflows/hooks`)}
          >
            Hook library
          </Button>}
          <Button
            size="sm"
            variant="outline"
            onClick={() => navigation.push(`/${workspace.slug}/workflows/runs`)}
          >
            Workflow log
          </Button>
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button className="md:hidden" size="sm" variant="outline" aria-label="Workflow menu" />}><Menu className="size-4" />Menu</DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {evalsEnabled && <DropdownMenuItem onClick={() => navigation.push(`/${workspace.slug}/workflows/evals`)}>Eval catalog</DropdownMenuItem>}
              {hooksEnabled && <DropdownMenuItem onClick={() => navigation.push(`/${workspace.slug}/workflows/hooks`)}>Hook library</DropdownMenuItem>}
              <DropdownMenuItem onClick={() => navigation.push(`/${workspace.slug}/workflows/runs`)}>Workflow log</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button size="sm" onClick={() => navigation.push(`/${workspace.slug}/workflows/new`)}><Plus className="size-4" />New workflow</Button>
        </div>
      </PageHeader>

      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="flex flex-col gap-4 p-6">
          {list.isError && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              Failed to load workflows. {list.error instanceof Error ? list.error.message : ""}
            </div>
          )}

          <div className="hidden md:block"><Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[28%]">Name</TableHead>
                <TableHead className="w-[22%]">Type</TableHead>
                <TableHead className="w-[22%]">Trigger / Action</TableHead>
                <TableHead className="w-[12%]">Active</TableHead>
                <TableHead className="w-[16%] text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isLoading && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                    Loading workflows…
                  </TableCell>
                </TableRow>
              )}
              {workflows.length === 0 && !list.isLoading && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-sm text-muted-foreground">
                    No workflows yet. Press <strong>New workflow</strong> to create a rule.
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
                        Created {new Date(wf.created_at).toLocaleString()}
                      </div>
                    </button>
                  </TableCell>
                  <TableCell className="text-xs">
                    {wf.workflow_type === "issue_loop" ? "Issue workflow" : "Standard"}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {wf.workflow_type === "issue_loop"
                      ? "Plan → Build → Delivery gate"
                      : `${wf.trigger_type} → ${wf.action_type}`}
                  </TableCell>
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
                        if (confirm(`Delete workflow "${wf.name}"?`)) remove.mutate(wf.id);
                      }}
                    >
                      Delete
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table></div>

          <div className="grid gap-3 md:hidden" aria-label="Workflows">
            {workflows.length === 0 && !list.isLoading && (
              <div className="rounded-lg border p-5 text-center text-sm text-muted-foreground">
                No workflows yet. Press <strong>New workflow</strong> to create a rule.
              </div>
            )}
            {workflows.map((wf) => (
              <article key={wf.id} className="grid min-w-0 gap-3 rounded-lg border bg-card p-4 text-card-foreground">
                <button type="button" className="min-w-0 text-left" onClick={() => navigation.push(`/${workspace.slug}/workflows/${wf.id}`)}>
                  <div className="truncate font-medium">{wf.name}</div>
                  <div className="text-[11px] text-muted-foreground">Created {new Date(wf.created_at).toLocaleString()}</div>
                </button>
                <div className="grid min-w-0 gap-1 text-xs">
                  <span>{wf.workflow_type === "issue_loop" ? "Issue workflow" : "Standard"}</span>
                  <span className="truncate font-mono text-muted-foreground">
                    {wf.workflow_type === "issue_loop" ? "Plan → Build → Delivery gate" : `${wf.trigger_type} → ${wf.action_type}`}
                  </span>
                </div>
                <div className="flex flex-wrap items-center gap-2 border-t pt-3">
                  <label className="mr-auto flex items-center gap-2 text-xs font-medium">
                    <Switch checked={wf.enabled} onCheckedChange={(value) => toggle.mutate({ id: wf.id, enabled: value === true })} disabled={toggle.isPending} />
                    Active
                  </label>
                  <Button size="sm" variant="ghost" onClick={() => navigation.push(`/${workspace.slug}/workflows/${wf.id}/runs`)}>Runs</Button>
                  <Button size="sm" variant="ghost" className="text-destructive" onClick={() => { if (confirm(`Delete workflow "${wf.name}"?`)) remove.mutate(wf.id); }}>Delete</Button>
                </div>
              </article>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

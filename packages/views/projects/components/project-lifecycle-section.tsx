"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ChevronUp, Settings2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  effectiveIssueLifecycleOptions,
  useArchiveIssueLifecycleStatus,
  useReorderIssueLifecycleStatuses,
  useUpdateIssueLifecycleStatus,
  useUpdateProjectIssueLifecycle,
} from "@multica/core/issue-lifecycles";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type {
  IssueLifecycleAssigneeTarget,
  IssueLifecycleEntryPolicy,
  IssueLifecycleExecutorTarget,
  IssueLifecyclePhase,
  IssueLifecycleStatusNode,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

function assigneeValue(target: IssueLifecycleAssigneeTarget) {
  return target.type === "keep" ? "keep" : `${target.type}:${target.id}`;
}

function executorValue(target: IssueLifecycleExecutorTarget) {
  return target.type === "none" ? "none" : `${target.type}:${target.id}`;
}

function parseTarget(value: string, kind: "assignee" | "executor") {
  if (value === "keep") return { type: "keep" } as IssueLifecycleAssigneeTarget;
  if (value === "none") return { type: "none" } as IssueLifecycleExecutorTarget;
  const [type, id] = value.split(":", 2);
  if (kind === "assignee") return { type, id } as IssueLifecycleAssigneeTarget;
  return { type, id } as IssueLifecycleExecutorTarget;
}

function StatusEditor({ node, revision, onClose }: {
  node: IssueLifecycleStatusNode;
  revision: number;
  onClose: () => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const update = useUpdateIssueLifecycleStatus();
  const [name, setName] = useState(node.name);
  const [description, setDescription] = useState(node.description);
  const [color, setColor] = useState(node.color);
  const [phase, setPhase] = useState<IssueLifecyclePhase>(node.phase as IssueLifecyclePhase);
  const [policy, setPolicy] = useState<IssueLifecycleEntryPolicy>(node.entry_policy);
  const assigneeItems = useMemo(() => [
    { value: "keep", label: t(($) => $.lifecycle.keep_assignee) },
    ...members.map((member) => ({ value: `human:${member.user_id}`, label: t(($) => $.lifecycle.human_target, { name: member.name }) })),
    ...agents.filter((agent) => !agent.archived_at).map((agent) => ({ value: `agent:${agent.id}`, label: t(($) => $.lifecycle.agent_target, { name: agent.name }) })),
    ...squads.filter((squad) => !squad.archived_at).map((squad) => ({ value: `squad:${squad.id}`, label: t(($) => $.lifecycle.squad_target, { name: squad.name }) })),
  ], [agents, members, squads, t]);
  const executorItems = useMemo(() => [
    { value: "none", label: t(($) => $.lifecycle.no_automatic_run) },
    ...agents.filter((agent) => !agent.archived_at).map((agent) => ({ value: `agent:${agent.id}`, label: t(($) => $.lifecycle.agent_target, { name: agent.name }) })),
    ...squads.filter((squad) => !squad.archived_at).map((squad) => ({ value: `squad:${squad.id}`, label: t(($) => $.lifecycle.squad_target, { name: squad.name }) })),
  ], [agents, squads, t]);
  const phaseItems: Array<{ value: IssueLifecyclePhase; label: string }> = [
    { value: "backlog", label: t(($) => $.lifecycle.phase_backlog) },
    { value: "unstarted", label: t(($) => $.lifecycle.phase_unstarted) },
    { value: "started", label: t(($) => $.lifecycle.phase_started) },
    { value: "completed", label: t(($) => $.lifecycle.phase_completed) },
    { value: "cancelled", label: t(($) => $.lifecycle.phase_cancelled) },
  ];
  const advanceItems = [
    { value: "human_confirms", label: t(($) => $.lifecycle.advance_human_confirms) },
    { value: "executor_may_transition", label: t(($) => $.lifecycle.advance_executor_may_transition) },
  ] as const;

  useEffect(() => {
    setName(node.name);
    setDescription(node.description);
    setColor(node.color);
    setPhase(node.phase as IssueLifecyclePhase);
    setPolicy(node.entry_policy);
  }, [node]);

  const canSave = name.trim().length > 0 && (
    policy.executor.type === "none" || policy.instructions.trim().length > 0
  );

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader><DialogTitle>{t(($) => $.lifecycle.edit_title)}</DialogTitle></DialogHeader>
        <div className="space-y-4">
          <div className="grid grid-cols-[1fr_84px] gap-2">
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t(($) => $.lifecycle.name_placeholder)} />
            <Input type="color" value={color} onChange={(event) => setColor(event.target.value)} className="px-2" />
          </div>
          <Textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={t(($) => $.lifecycle.description_placeholder)} />
          <label className="block space-y-1.5 text-body">
            <span className="text-muted-foreground">{t(($) => $.lifecycle.phase_label)}</span>
            <Select items={phaseItems} value={phase} onValueChange={(value) => { if (value) setPhase(value); }}>
              <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>{phaseItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
            </Select>
          </label>
          <div className="rounded-lg border p-3 space-y-3">
            <div className="font-medium text-body">{t(($) => $.lifecycle.on_entry)}</div>
            <label className="block space-y-1.5 text-body">
              <span className="text-muted-foreground">{t(($) => $.lifecycle.assign_to)}</span>
              <Select
                items={assigneeItems}
                value={assigneeValue(policy.assignee)}
                onValueChange={(value) => { if (value) setPolicy((current) => ({ ...current, assignee: parseTarget(value, "assignee") as IssueLifecycleAssigneeTarget })); }}
              >
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {assigneeItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </label>
            <label className="block space-y-1.5 text-body">
              <span className="text-muted-foreground">{t(($) => $.lifecycle.run)}</span>
              <Select
                items={executorItems}
                value={executorValue(policy.executor)}
                onValueChange={(value) => setPolicy((current) => {
                  const executor = parseTarget(value ?? "none", "executor") as IssueLifecycleExecutorTarget;
                  return {
                    ...current,
                    executor,
                    advance: executor.type === "none" ? "human_confirms" : current.advance,
                  };
                })}
              >
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {executorItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                </SelectContent>
              </Select>
            </label>
            {policy.executor.type !== "none" && (
              <>
                <Textarea
                  value={policy.instructions}
                  onChange={(event) => setPolicy((current) => ({ ...current, instructions: event.target.value }))}
                  placeholder={t(($) => $.lifecycle.instructions_placeholder)}
                  rows={5}
                />
                <Select
                  items={advanceItems}
                  value={policy.advance}
                  onValueChange={(value) => { if (value) setPolicy((current) => ({ ...current, advance: value })); }}
                >
                  <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {advanceItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{t(($) => $.lifecycle.cancel)}</Button>
          <Button
            disabled={!canSave || update.isPending}
            onClick={() => update.mutate({
              lifecycleId: node.lifecycle_id,
              statusId: node.id,
              data: {
                expected_revision: revision,
                name: name.trim(),
                description: description.trim(),
                color,
                phase,
                entry_policy: policy,
              },
            }, {
              onSuccess: () => { toast.success(t(($) => $.lifecycle.toast_updated)); onClose(); },
            })}
          >{t(($) => $.lifecycle.save)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectLifecycleSection({ projectId, canEdit }: { projectId: string; canEdit: boolean }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(effectiveIssueLifecycleOptions(wsId, projectId, true));
  const updateMode = useUpdateProjectIssueLifecycle();
  const reorder = useReorderIssueLifecycleStatuses();
  const archive = useArchiveIssueLifecycleStatus();
  const [open, setOpen] = useState(true);
  const [editing, setEditing] = useState<IssueLifecycleStatusNode | null>(null);
  const statuses = useMemo(() => (data?.statuses ?? []).filter((status) => !status.archived_at), [data?.statuses]);

  const move = (index: number, delta: number) => {
    if (!data) return;
    const next = [...statuses];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    const current = next[index];
    const destination = next[target];
    if (!current || !destination) return;
    next[index] = destination;
    next[target] = current;
    reorder.mutate({ lifecycleId: data.lifecycle.id, statusIds: next.map((status) => status.id), expectedRevision: data.lifecycle.revision });
  };

  return (
    <div>
      <button type="button" className="mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium hover:bg-accent/70" onClick={() => setOpen((value) => !value)}>
        {t(($) => $.lifecycle.title)}
        <ChevronRight className={`!size-3 text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="space-y-2 pl-2">
          <div className="flex items-center justify-between gap-2 text-caption">
            <span className="text-muted-foreground">{isLoading ? t(($) => $.lifecycle.loading) : data?.mode === "custom" ? t(($) => $.lifecycle.mode_custom) : t(($) => $.lifecycle.mode_default)}</span>
            {canEdit && data && (
              <Button
                size="xs"
                variant="outline"
                disabled={updateMode.isPending}
                onClick={() => updateMode.mutate({ projectId, mode: data.mode === "custom" ? "default" : "custom" })}
              >{data.mode === "custom" ? t(($) => $.lifecycle.use_default) : t(($) => $.lifecycle.customize)}</Button>
            )}
          </div>
          <div className="overflow-hidden rounded-lg border">
            {statuses.map((status, index) => (
              <div key={status.id} className="group flex min-h-8 items-center gap-2 border-b px-2 last:border-b-0">
                <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: status.color }} />
                <button type="button" className="min-w-0 flex-1 truncate text-left text-caption" disabled={!canEdit || data?.mode !== "custom"} onClick={() => setEditing(status)}>
                  {status.name}
                </button>
                {canEdit && data?.mode === "custom" && (
                  <div className="flex items-center opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
                    <button type="button" aria-label={t(($) => $.lifecycle.move_up)} disabled={index === 0} className="rounded p-0.5 hover:bg-accent disabled:opacity-30" onClick={() => move(index, -1)}><ChevronUp className="size-3" /></button>
                    <button type="button" aria-label={t(($) => $.lifecycle.move_down)} disabled={index === statuses.length - 1} className="rounded p-0.5 hover:bg-accent disabled:opacity-30" onClick={() => move(index, 1)}><ChevronDown className="size-3" /></button>
                    <button type="button" aria-label={t(($) => $.lifecycle.edit_status)} className="rounded p-0.5 hover:bg-accent" onClick={() => setEditing(status)}><Settings2 className="size-3" /></button>
                    <button
                      type="button"
                      aria-label={t(($) => $.lifecycle.archive_status)}
                      disabled={statuses.length <= 1}
                      className="rounded p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-30"
                      onClick={() => archive.mutate({ lifecycleId: data.lifecycle.id, statusId: status.id, expectedRevision: data.lifecycle.revision })}
                    ><Trash2 className="size-3" /></button>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
      {editing && data && <StatusEditor node={editing} revision={data.lifecycle.revision} onClose={() => setEditing(null)} />}
    </div>
  );
}

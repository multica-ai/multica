"use client";

import { useState, useEffect, useCallback } from "react";
import { Plus, Play, Pause, Clock, Zap, Trash2, MoreHorizontal, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import { useAgentflowStore } from "../store";
import type {
  Agentflow,
  AgentflowTrigger,
  AgentflowRun,
  AgentflowConcurrencyPolicy,
} from "@/shared/types";

function formatDate(dateStr: string | null): string {
  if (!dateStr) return "—";
  return new Date(dateStr).toLocaleString();
}

function StatusBadge({ status }: { status: string }) {
  const variants: Record<string, string> = {
    active: "bg-green-100 text-green-800",
    paused: "bg-yellow-100 text-yellow-800",
    archived: "bg-gray-100 text-gray-600",
  };
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${variants[status] ?? "bg-gray-100 text-gray-600"}`}>
      {status}
    </span>
  );
}

function RunStatusBadge({ status }: { status: string }) {
  const variants: Record<string, string> = {
    received: "bg-blue-100 text-blue-800",
    executing: "bg-yellow-100 text-yellow-800",
    completed: "bg-green-100 text-green-800",
    failed: "bg-red-100 text-red-800",
    skipped: "bg-gray-100 text-gray-600",
    coalesced: "bg-gray-100 text-gray-600",
  };
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${variants[status] ?? "bg-gray-100 text-gray-600"}`}>
      {status}
    </span>
  );
}

// ── Create Dialog ──────────────────────────────────────────────

function CreateAgentflowDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const agents = useWorkspaceStore((s) => s.agents);
  const addAgentflow = useAgentflowStore((s) => s.addAgentflow);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [agentId, setAgentId] = useState("");
  const [concurrencyPolicy, setConcurrencyPolicy] = useState<AgentflowConcurrencyPolicy>("skip_if_active");
  const [cronExpression, setCronExpression] = useState("");
  const [timezone, setTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    if (!title.trim() || !agentId) return;
    setCreating(true);
    try {
      const af = await api.createAgentflow({
        title: title.trim(),
        description: description.trim() || undefined,
        agent_id: agentId,
        concurrency_policy: concurrencyPolicy,
      });
      addAgentflow(af);

      // Create schedule trigger if cron was provided
      if (cronExpression.trim()) {
        await api.createAgentflowTrigger(af.id, {
          kind: "schedule",
          cron_expression: cronExpression.trim(),
          timezone,
        });
      }

      toast.success("Agentflow created");
      onClose();
      setTitle("");
      setDescription("");
      setAgentId("");
      setCronExpression("");
    } catch {
      toast.error("Failed to create agentflow");
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Create Agentflow</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div className="space-y-2">
            <Label>Title</Label>
            <Input
              placeholder="e.g. Daily SSL Check"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label>Agent</Label>
            <Select value={agentId} onValueChange={(v) => v && setAgentId(v)}>
              <SelectTrigger>
                <SelectValue placeholder="Select an agent" />
              </SelectTrigger>
              <SelectContent>
                {agents
                  .filter((a) => !a.archived_at)
                  .map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>Prompt / Instructions</Label>
            <Textarea
              placeholder="What should the agent do when this agentflow triggers?"
              rows={4}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label>Schedule (cron expression)</Label>
            <Input
              placeholder="e.g. 0 10 * * 1-5 (weekdays at 10am)"
              value={cronExpression}
              onChange={(e) => setCronExpression(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Standard 5-field cron format. Leave empty to trigger manually only.
            </p>
          </div>
          <div className="space-y-2">
            <Label>Timezone</Label>
            <Input
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label>Concurrency Policy</Label>
            <Select value={concurrencyPolicy} onValueChange={(v) => setConcurrencyPolicy(v as AgentflowConcurrencyPolicy)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="skip_if_active">Skip if active</SelectItem>
                <SelectItem value="coalesce">Coalesce</SelectItem>
                <SelectItem value="always_run">Always run</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={handleCreate} disabled={creating || !title.trim() || !agentId}>
            {creating ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Detail Panel ──────────────────────────────────────────────

function AgentflowDetail({ agentflow }: { agentflow: Agentflow }) {
  const agents = useWorkspaceStore((s) => s.agents);
  const { triggers, runs, fetchTriggers, fetchRuns, updateAgentflow } = useAgentflowStore();

  const afTriggers = triggers[agentflow.id] ?? [];
  const afRuns = runs[agentflow.id] ?? [];
  const agent = agents.find((a) => a.id === agentflow.agent_id);

  useEffect(() => {
    fetchTriggers(agentflow.id);
    fetchRuns(agentflow.id);
  }, [agentflow.id, fetchTriggers, fetchRuns]);

  const handleRun = async () => {
    try {
      await api.runAgentflow(agentflow.id);
      toast.success("Agentflow triggered");
      fetchRuns(agentflow.id);
    } catch {
      toast.error("Failed to trigger agentflow");
    }
  };

  const handleToggleStatus = async () => {
    const newStatus = agentflow.status === "active" ? "paused" : "active";
    try {
      const updated = await api.updateAgentflow(agentflow.id, { status: newStatus });
      updateAgentflow(agentflow.id, updated);
      toast.success(`Agentflow ${newStatus}`);
    } catch {
      toast.error("Failed to update status");
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between p-4 border-b">
        <div>
          <h2 className="text-lg font-semibold">{agentflow.title}</h2>
          <p className="text-sm text-muted-foreground">
            Agent: {agent?.name ?? "Unknown"} · <StatusBadge status={agentflow.status} />
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleToggleStatus}>
            {agentflow.status === "active" ? (
              <><Pause className="h-4 w-4 mr-1" /> Pause</>
            ) : (
              <><Play className="h-4 w-4 mr-1" /> Activate</>
            )}
          </Button>
          <Button size="sm" onClick={handleRun}>
            <Zap className="h-4 w-4 mr-1" /> Run Now
          </Button>
        </div>
      </div>

      <Tabs defaultValue="triggers" className="flex-1 overflow-hidden flex flex-col">
        <TabsList className="mx-4 mt-2">
          <TabsTrigger value="triggers">Triggers</TabsTrigger>
          <TabsTrigger value="runs">Runs</TabsTrigger>
          <TabsTrigger value="details">Details</TabsTrigger>
        </TabsList>

        <TabsContent value="triggers" className="flex-1 overflow-auto p-4">
          {afTriggers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No triggers configured. This agentflow can only be triggered manually.</p>
          ) : (
            <div className="space-y-3">
              {afTriggers.map((t) => (
                <TriggerCard key={t.id} trigger={t} onRefresh={() => fetchTriggers(agentflow.id)} />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="runs" className="flex-1 overflow-auto p-4">
          {afRuns.length === 0 ? (
            <p className="text-sm text-muted-foreground">No runs yet.</p>
          ) : (
            <div className="space-y-2">
              {afRuns.map((r) => (
                <div key={r.id} className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
                  <div className="flex items-center gap-2">
                    <RunStatusBadge status={r.status} />
                    <span className="text-muted-foreground">{r.source_kind}</span>
                  </div>
                  <div className="text-muted-foreground text-xs">
                    {formatDate(r.created_at)}
                  </div>
                </div>
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="details" className="flex-1 overflow-auto p-4">
          <div className="space-y-4">
            <div>
              <Label className="text-xs text-muted-foreground">Prompt / Instructions</Label>
              <p className="mt-1 text-sm whitespace-pre-wrap">{agentflow.description || "No description"}</p>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Concurrency Policy</Label>
              <p className="mt-1 text-sm">{agentflow.concurrency_policy}</p>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground">Created</Label>
              <p className="mt-1 text-sm">{formatDate(agentflow.created_at)}</p>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function TriggerCard({ trigger, onRefresh }: { trigger: AgentflowTrigger; onRefresh: () => void }) {
  const handleToggle = async () => {
    try {
      await api.updateAgentflowTrigger(trigger.id, { enabled: !trigger.enabled });
      onRefresh();
    } catch {
      toast.error("Failed to update trigger");
    }
  };

  const handleDelete = async () => {
    try {
      await api.deleteAgentflowTrigger(trigger.id);
      toast.success("Trigger deleted");
      onRefresh();
    } catch {
      toast.error("Failed to delete trigger");
    }
  };

  return (
    <div className="flex items-center justify-between rounded-md border px-3 py-2">
      <div className="flex items-center gap-3">
        <Clock className="h-4 w-4 text-muted-foreground" />
        <div>
          <p className="text-sm font-medium">{trigger.kind}: {trigger.cron_expression ?? "—"}</p>
          <p className="text-xs text-muted-foreground">
            {trigger.timezone ?? "UTC"} · Next: {formatDate(trigger.next_run_at)} · {trigger.enabled ? "Enabled" : "Disabled"}
          </p>
        </div>
      </div>
      <div className="flex items-center gap-1">
        <Button variant="ghost" size="sm" onClick={handleToggle}>
          {trigger.enabled ? "Disable" : "Enable"}
        </Button>
        <Button variant="ghost" size="sm" onClick={handleDelete}>
          <Trash2 className="h-4 w-4 text-destructive" />
        </Button>
      </div>
    </div>
  );
}

// ── Main Page ──────────────────────────────────────────────────

export function AgentflowsPage() {
  const { agentflows, loading, activeAgentflowId, fetch, setActiveAgentflow } = useAgentflowStore();
  const agents = useWorkspaceStore((s) => s.agents);
  const [showCreate, setShowCreate] = useState(false);

  useEffect(() => {
    fetch();
  }, [fetch]);

  const activeAgentflow = agentflows.find((af) => af.id === activeAgentflowId) ?? null;

  return (
    <div className="flex h-full">
      {/* List panel */}
      <div className="w-80 border-r flex flex-col">
        <div className="flex items-center justify-between p-4 border-b">
          <h1 className="text-lg font-semibold">Agentflows</h1>
          <Button size="sm" onClick={() => setShowCreate(true)}>
            <Plus className="h-4 w-4 mr-1" /> New
          </Button>
        </div>

        <div className="flex-1 overflow-auto">
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : agentflows.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-center px-4">
              <Zap className="h-8 w-8 text-muted-foreground mb-2" />
              <p className="text-sm text-muted-foreground">No agentflows yet</p>
              <p className="text-xs text-muted-foreground mt-1">Create one to schedule recurring agent tasks</p>
            </div>
          ) : (
            <div className="divide-y">
              {agentflows.map((af) => {
                const agent = agents.find((a) => a.id === af.agent_id);
                return (
                  <button
                    key={af.id}
                    onClick={() => setActiveAgentflow(af.id)}
                    className={`w-full text-left px-4 py-3 hover:bg-accent/50 transition-colors ${
                      activeAgentflowId === af.id ? "bg-accent" : ""
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-sm font-medium truncate">{af.title}</span>
                      <StatusBadge status={af.status} />
                    </div>
                    <p className="text-xs text-muted-foreground mt-0.5">
                      {agent?.name ?? "Unknown agent"}
                    </p>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Detail panel */}
      <div className="flex-1">
        {activeAgentflow ? (
          <AgentflowDetail agentflow={activeAgentflow} />
        ) : (
          <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
            Select an agentflow to view details
          </div>
        )}
      </div>

      <CreateAgentflowDialog open={showCreate} onClose={() => setShowCreate(false)} />
    </div>
  );
}

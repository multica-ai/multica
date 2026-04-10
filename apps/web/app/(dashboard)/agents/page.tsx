"use client";

import { useState, useEffect, useRef, useMemo } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import {
  Bot,
  Cloud,
  Monitor,
  Plus,
  ListTodo,
  FileText,
  BookOpenText,
  Trash2,
  Save,
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  AlertCircle,
  MoreHorizontal,
  Play,
  ChevronDown,
  Globe,
  Lock,
  Settings,
  Camera,
  Archive,
  Braces,
  List,
} from "lucide-react";
import type {
  Agent,
  AgentStatus,
  AgentVisibility,
  AgentTask,
  AgentRuntimeConfig,
  RuntimeDevice,
  CreateAgentRequest,
  UpdateAgentRequest,
} from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@/platform/api";
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import { skillListOptions, agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const statusConfig: Record<AgentStatus, { label: string; color: string; dot: string }> = {
  idle: { label: "Idle", color: "text-muted-foreground", dot: "bg-muted-foreground" },
  working: { label: "Working", color: "text-success", dot: "bg-success" },
  blocked: { label: "Blocked", color: "text-warning", dot: "bg-warning" },
  error: { label: "Error", color: "text-destructive", dot: "bg-destructive" },
  offline: { label: "Offline", color: "text-muted-foreground/50", dot: "bg-muted-foreground/40" },
};

const taskStatusConfig: Record<string, { label: string; icon: typeof CheckCircle2; color: string }> = {
  queued: { label: "Queued", icon: Clock, color: "text-muted-foreground" },
  dispatched: { label: "Dispatched", icon: Play, color: "text-info" },
  running: { label: "Running", icon: Loader2, color: "text-success" },
  completed: { label: "Completed", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  cancelled: { label: "Cancelled", icon: XCircle, color: "text-muted-foreground" },
};


function generateId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

function getRuntimeDevice(agent: Agent, runtimes: RuntimeDevice[]): RuntimeDevice | undefined {
  return runtimes.find((runtime) => runtime.id === agent.runtime_id);
}

// ---------------------------------------------------------------------------
// Create Agent Dialog
// ---------------------------------------------------------------------------

function CreateAgentDialog({
  runtimes,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  onClose: () => void;
  onCreate: (data: CreateAgentRequest) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(runtimes[0]?.id ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>("private");
  const [creating, setCreating] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);

  useEffect(() => {
    if (!selectedRuntimeId && runtimes[0]) {
      setSelectedRuntimeId(runtimes[0].id);
    }
  }, [runtimes, selectedRuntimeId]);

  const selectedRuntime = runtimes.find((d) => d.id === selectedRuntimeId) ?? null;

  const handleSubmit = async () => {
    if (!name.trim() || !selectedRuntime) return;
    setCreating(true);
    try {
      await onCreate({
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        visibility,
      });
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create agent");
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create Agent</DialogTitle>
          <DialogDescription>
            Create a new AI agent for your workspace.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div>
            <Label className="text-xs text-muted-foreground">Name</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Deep Research Agent"
              className="mt-1"
              onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this agent do?"
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Visibility</Label>
            <div className="mt-1.5 flex gap-2">
              <button
                type="button"
                onClick={() => setVisibility("workspace")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                  visibility === "workspace"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Workspace</div>
                  <div className="text-xs text-muted-foreground">All members can assign</div>
                </div>
              </button>
              <button
                type="button"
                onClick={() => setVisibility("private")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                  visibility === "private"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Private</div>
                  <div className="text-xs text-muted-foreground">Only you can assign</div>
                </div>
              </button>
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Runtime</Label>
            <Popover open={runtimeOpen} onOpenChange={setRuntimeOpen}>
              <PopoverTrigger
                disabled={runtimes.length === 0}
                className="flex w-full items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
              >
                {selectedRuntime?.runtime_mode === "cloud" ? (
                  <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {selectedRuntime?.name ?? "No runtime available"}
                    </span>
                    {selectedRuntime?.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        Cloud
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {selectedRuntime?.device_info ?? "Register a runtime before creating an agent"}
                  </div>
                </div>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${runtimeOpen ? "rotate-180" : ""}`} />
              </PopoverTrigger>
              <PopoverContent align="start" className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto">
                {runtimes.map((device) => (
                  <button
                    key={device.id}
                    onClick={() => {
                      setSelectedRuntimeId(device.id);
                      setRuntimeOpen(false);
                    }}
                    className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                      device.id === selectedRuntimeId ? "bg-accent" : "hover:bg-accent/50"
                    }`}
                  >
                    {device.runtime_mode === "cloud" ? (
                      <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
                    )}
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{device.name}</span>
                        {device.runtime_mode === "cloud" && (
                          <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                            Cloud
                          </span>
                        )}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">{device.device_info}</div>
                    </div>
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        device.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                      }`}
                    />
                  </button>
                ))}
              </PopoverContent>
            </Popover>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={creating || !name.trim() || !selectedRuntime}
          >
            {creating ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Agent List Item
// ---------------------------------------------------------------------------

function AgentListItem({
  agent,
  isSelected,
  onClick,
}: {
  agent: Agent;
  isSelected: boolean;
  onClick: () => void;
}) {
  const st = statusConfig[agent.status];
  const isArchived = !!agent.archived_at;

  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <ActorAvatar actorType="agent" actorId={agent.id} size={32} className={`rounded-lg ${isArchived ? "opacity-50 grayscale" : ""}`} />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className={`truncate text-sm font-medium ${isArchived ? "text-muted-foreground" : ""}`}>{agent.name}</span>
          {agent.runtime_mode === "cloud" ? (
            <Cloud className="h-3 w-3 text-muted-foreground" />
          ) : (
            <Monitor className="h-3 w-3 text-muted-foreground" />
          )}
        </div>
        <div className="flex items-center gap-1.5 mt-0.5">
          {isArchived ? (
            <span className="text-xs text-muted-foreground">Archived</span>
          ) : (
            <>
              <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
              <span className={`text-xs ${st.color}`}>{st.label}</span>
            </>
          )}
        </div>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Instructions Tab
// ---------------------------------------------------------------------------

function InstructionsTab({
  agent,
  onSave,
}: {
  agent: Agent;
  onSave: (instructions: string) => Promise<void>;
}) {
  const [value, setValue] = useState(agent.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const isDirty = value !== (agent.instructions ?? "");

  // Sync when switching between agents.
  useEffect(() => {
    setValue(agent.instructions ?? "");
  }, [agent.id, agent.instructions]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(value);
    } catch {
      // toast handled by parent
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">Agent Instructions</h3>
        <p className="text-xs text-muted-foreground mt-0.5">
          Define this agent&apos;s identity and working style. These instructions are
          injected into the agent&apos;s context for every task.
        </p>
      </div>

      <textarea
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder={`Define this agent's role, expertise, and working style.\n\nExample:\nYou are a frontend engineer specializing in React and TypeScript.\n\n## Working Style\n- Write small, focused PRs — one commit per logical change\n- Prefer composition over inheritance\n- Always add unit tests for new components\n\n## Constraints\n- Do not modify shared/ types without explicit approval\n- Follow the existing component patterns in features/`}
        className="w-full min-h-[300px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y"
      />

      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {value.length > 0 ? `${value.length} characters` : "No instructions set"}
        </span>
        <Button
          size="xs"
          onClick={handleSave}
          disabled={!isDirty || saving}
        >
          {saving ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Save className="h-3 w-3" />
          )}
          Save
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Skills Tab (picker — skills are managed on /skills page)
// ---------------------------------------------------------------------------

function SkillsTab({
  agent,
}: {
  agent: Agent;
}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const [saving, setSaving] = useState(false);
  const [showPicker, setShowPicker] = useState(false);

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableSkills = workspaceSkills.filter((s) => !agentSkillIds.has(s.id));

  const handleAdd = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), skillId];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add skill");
    } finally {
      setSaving(false);
      setShowPicker(false);
    }
  };

  const handleRemove = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = agent.skills.filter((s) => s.id !== skillId).map((s) => s.id);
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove skill");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">Skills</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            Reusable skills assigned to this agent. Manage skills on the Skills page.
          </p>
        </div>
        <Button
          variant="outline"
          size="xs"
          onClick={() => setShowPicker(true)}
          disabled={saving || availableSkills.length === 0}
        >
          <Plus className="h-3 w-3" />
          Add Skill
        </Button>
      </div>

      {agent.skills.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <FileText className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No skills assigned</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Add skills from the workspace to this agent.
          </p>
          {availableSkills.length > 0 && (
            <Button
              onClick={() => setShowPicker(true)}
              size="xs"
              className="mt-3"
              disabled={saving}
            >
              <Plus className="h-3 w-3" />
              Add Skill
            </Button>
          )}
        </div>
      ) : (
        <div className="space-y-2">
          {agent.skills.map((skill) => (
            <div
              key={skill.id}
              className="flex items-center gap-3 rounded-lg border px-4 py-3"
            >
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                <FileText className="h-4 w-4 text-muted-foreground" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{skill.name}</div>
                {skill.description && (
                  <div className="text-xs text-muted-foreground truncate">
                    {skill.description}
                  </div>
                )}
              </div>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => handleRemove(skill.id)}
                disabled={saving}
                className="text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      {/* Skill Picker Dialog */}
      {showPicker && (
        <Dialog open onOpenChange={(v) => { if (!v) setShowPicker(false); }}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="text-sm">Add Skill</DialogTitle>
              <DialogDescription className="text-xs">
                Select a skill to assign to this agent.
              </DialogDescription>
            </DialogHeader>
            <div className="max-h-64 overflow-y-auto space-y-1">
              {availableSkills.map((skill) => (
                <button
                  key={skill.id}
                  onClick={() => handleAdd(skill.id)}
                  disabled={saving}
                  className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50"
                >
                  <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{skill.name}</div>
                    {skill.description && (
                      <div className="text-xs text-muted-foreground truncate">
                        {skill.description}
                      </div>
                    )}
                  </div>
                </button>
              ))}
              {availableSkills.length === 0 && (
                <p className="py-6 text-center text-xs text-muted-foreground">
                  All workspace skills are already assigned.
                </p>
              )}
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setShowPicker(false)}>
                Cancel
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tasks Tab
// ---------------------------------------------------------------------------

function TasksTab({ agent }: { agent: Agent }) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [loading, setLoading] = useState(true);
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));

  useEffect(() => {
    setLoading(true);
    api
      .listAgentTasks(agent.id)
      .then(setTasks)
      .catch(() => setTasks([]))
      .finally(() => setLoading(false));
  }, [agent.id]);

  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 rounded-lg border px-4 py-3">
            <Skeleton className="h-4 w-4 rounded shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    );
  }

  // Sort: active tasks (running > dispatched > queued) first, then completed/failed by date
  const activeStatuses = ["running", "dispatched", "queued"];
  const sortedTasks = [...tasks].sort((a, b) => {
    const aActive = activeStatuses.indexOf(a.status);
    const bActive = activeStatuses.indexOf(b.status);
    const aIsActive = aActive !== -1;
    const bIsActive = bActive !== -1;
    if (aIsActive && !bIsActive) return -1;
    if (!aIsActive && bIsActive) return 1;
    if (aIsActive && bIsActive) return aActive - bActive;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  const issueMap = new Map(issues.map((i) => [i.id, i]));

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">Task Queue</h3>
        <p className="text-xs text-muted-foreground mt-0.5">
          Issues assigned to this agent and their execution status.
        </p>
      </div>

      {tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <ListTodo className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No tasks in queue</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Assign an issue to this agent to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {sortedTasks.map((task) => {
            const config = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
            const Icon = config.icon;
            const issue = issueMap.get(task.issue_id);
            const isActive = task.status === "running" || task.status === "dispatched";
            const isRunning = task.status === "running";

            return (
              <div
                key={task.id}
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 ${
                  isRunning
                    ? "border-success/40 bg-success/5"
                    : task.status === "dispatched"
                      ? "border-info/40 bg-info/5"
                      : ""
                }`}
              >
                <Icon
                  className={`h-4 w-4 shrink-0 ${config.color} ${
                    isRunning ? "animate-spin" : ""
                  }`}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    {issue && (
                      <span className="shrink-0 text-xs font-mono text-muted-foreground">
                        {issue.identifier}
                      </span>
                    )}
                    <span className={`text-sm truncate ${isActive ? "font-medium" : ""}`}>
                      {issue?.title ?? `Issue ${task.issue_id.slice(0, 8)}...`}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground mt-0.5">
                    {isRunning && task.started_at
                      ? `Started ${new Date(task.started_at).toLocaleString()}`
                      : task.status === "dispatched" && task.dispatched_at
                        ? `Dispatched ${new Date(task.dispatched_at).toLocaleString()}`
                        : task.status === "completed" && task.completed_at
                          ? `Completed ${new Date(task.completed_at).toLocaleString()}`
                          : task.status === "failed" && task.completed_at
                            ? `Failed ${new Date(task.completed_at).toLocaleString()}`
                            : `Queued ${new Date(task.created_at).toLocaleString()}`}
                  </div>
                </div>
                <span className={`shrink-0 text-xs font-medium ${config.color}`}>
                  {config.label}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Settings Tab
// ---------------------------------------------------------------------------

function SettingsTab({
  agent,
  runtimes,
  onSave,
}: {
  agent: Agent;
  runtimes: RuntimeDevice[];
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const [name, setName] = useState(agent.name);
  const [description, setDescription] = useState(agent.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(agent.visibility);
  const [maxTasks, setMaxTasks] = useState(agent.max_concurrent_tasks);
  const [envVars, setEnvVars] = useState<{ key: string; value: string }[]>(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    const vars = rc?.env_vars;
    if (!vars || Object.keys(vars).length === 0) return [];
    return Object.entries(vars).map(([key, value]) => ({ key, value: String(value) }));
  });
  const [envMode, setEnvMode] = useState<"list" | "json">("list");
  const [jsonText, setJsonText] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [codexConfigToml, setCodexConfigToml] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.codex_config_toml ?? "";
  });
  const [codexDocsOpen, setCodexDocsOpen] = useState(false);
  const codexTomlRef = useRef<HTMLTextAreaElement>(null);
  const runtimeProvider = runtimes.find((r) => r.id === agent.runtime_id)?.provider ?? "";
  const [configMode, setConfigMode] = useState<"global" | "project">(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.config_mode === "project" ? "project" : "global";
  });
  const [claudeSettingsJson, setClaudeSettingsJson] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.claude_settings_json ?? "";
  });
  const [claudeDocsOpen, setClaudeDocsOpen] = useState(false);
  const claudeSettingsRef = useRef<HTMLTextAreaElement>(null);
  const [opencodeConfigJson, setOpencodeConfigJson] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.opencode_config_json ?? "";
  });
  const [opencodeDocsOpen, setOpencodeDocsOpen] = useState(false);
  const opencodeConfigRef = useRef<HTMLTextAreaElement>(null);
  const { upload, uploading } = useFileUpload(api);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const jsonTextareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-resize JSON textarea when content changes.
  useEffect(() => {
    const el = jsonTextareaRef.current;
    if (el && envMode === "json") {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, [jsonText, envMode]);

  // Auto-resize codex config.toml textarea.
  useEffect(() => {
    const el = codexTomlRef.current;
    if (el && runtimeProvider === "codex" && configMode === "project") {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, [codexConfigToml, configMode, runtimeProvider]);

  // Auto-resize claude settings.json textarea.
  useEffect(() => {
    const el = claudeSettingsRef.current;
    if (el && runtimeProvider === "claude" && configMode === "project") {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, [claudeSettingsJson, configMode, runtimeProvider]);

  // Auto-resize opencode config.json textarea.
  useEffect(() => {
    const el = opencodeConfigRef.current;
    if (el && runtimeProvider === "opencode" && configMode === "project") {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, [opencodeConfigJson, configMode, runtimeProvider]);

  const handleAvatarUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    try {
      const result = await upload(file);
      if (!result) return;
      await onSave({ avatar_url: result.link });
      toast.success("Avatar updated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to upload avatar");
    }
  };

  const buildRuntimeConfig = (): Record<string, unknown> | null => {
    const rc: Record<string, unknown> = {};
    if ((runtimeProvider === "claude" || runtimeProvider === "codex" || runtimeProvider === "opencode") && configMode !== "global") {
      rc.config_mode = configMode;
    }
    // Env vars are shown for: non-claude/codex/opencode providers always, or codex in project mode.
    // Claude/OpenCode project modes use their own config files instead of env vars.
    const showEnvVars = (runtimeProvider !== "claude" && runtimeProvider !== "codex" && runtimeProvider !== "opencode") || (runtimeProvider === "codex" && configMode === "project");
    if (showEnvVars) {
      if (envMode === "json") {
        if (!jsonText.trim() || jsonText.trim() === "{}") {
          // no env vars, but still might have provider config
        } else {
          const result = tryParseJsonEnvVars(jsonText);
          if (!result.ok) return null;
          const vars: Record<string, string> = {};
          result.vars.forEach((ev) => {
            if (ev.key.trim()) vars[ev.key.trim()] = ev.value;
          });
          if (Object.keys(vars).length > 0) rc.env_vars = vars;
        }
      } else {
        const vars: Record<string, string> = {};
        envVars.forEach((ev) => {
          if (ev.key.trim()) vars[ev.key.trim()] = ev.value;
        });
        if (Object.keys(vars).length > 0) rc.env_vars = vars;
      }
    }
    // Claude project mode: include settings.json content.
    if (runtimeProvider === "claude" && configMode === "project" && claudeSettingsJson.trim()) {
      rc.claude_settings_json = claudeSettingsJson;
    }
    // Codex project mode: include config.toml content.
    if (runtimeProvider === "codex" && configMode === "project" && codexConfigToml.trim()) {
      rc.codex_config_toml = codexConfigToml;
    }
    // OpenCode project mode: include opencode.json content.
    if (runtimeProvider === "opencode" && configMode === "project" && opencodeConfigJson.trim()) {
      rc.opencode_config_json = opencodeConfigJson;
    }
    return rc;
  };

  const envVarsToJson = (vars: { key: string; value: string }[]): string => {
    const obj: Record<string, string> = {};
    vars.forEach((ev) => {
      if (ev.key.trim()) obj[ev.key.trim()] = ev.value;
    });
    return JSON.stringify(obj, null, 2);
  };

  const tryParseJsonEnvVars = (text: string): { ok: true; vars: { key: string; value: string }[] } | { ok: false; error: string } => {
    try {
      const parsed = JSON.parse(text);
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
        return { ok: false, error: "Must be a JSON object" };
      }
      for (const [k, v] of Object.entries(parsed)) {
        if (typeof v !== "string") {
          return { ok: false, error: `Value for "${k}" must be a string` };
        }
      }
      return { ok: true, vars: Object.entries(parsed as Record<string, string>).map(([key, value]) => ({ key, value })) };
    } catch {
      return { ok: false, error: "Invalid JSON" };
    }
  };

  const switchToJson = () => {
    setJsonText(envVarsToJson(envVars));
    setJsonError(null);
    setEnvMode("json");
  };

  const switchToList = () => {
    if (!jsonText.trim() || jsonText.trim() === "{}") {
      setEnvVars([]);
      setJsonError(null);
      setEnvMode("list");
      return;
    }
    const result = tryParseJsonEnvVars(jsonText);
    if (result.ok) {
      setEnvVars(result.vars);
      setJsonError(null);
      setEnvMode("list");
    } else {
      setJsonError(result.error);
    }
  };

  const dirty =
    name !== agent.name ||
    description !== (agent.description ?? "") ||
    visibility !== agent.visibility ||
    maxTasks !== agent.max_concurrent_tasks ||
    JSON.stringify(buildRuntimeConfig()) !== JSON.stringify(agent.runtime_config ?? {});

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Name is required");
      return;
    }
    const rc = buildRuntimeConfig();
    if (rc === null) {
      if (envMode === "json") {
        const result = tryParseJsonEnvVars(jsonText);
        if (!result.ok) setJsonError(result.error);
      }
      toast.error("Invalid JSON in environment variables");
      return;
    }
    setSaving(true);
    try {
      await onSave({
        name: name.trim(),
        description,
        visibility,
        max_concurrent_tasks: maxTasks,
        runtime_config: rc,
      });
      toast.success("Settings saved");
    } catch {
      toast.error("Failed to save settings");
    } finally {
      setSaving(false);
    }
  };

  const runtimeDevice = runtimes.find((r) => r.id === agent.runtime_id);

  return (
    <div className="max-w-lg space-y-6">
      <div>
        <Label className="text-xs text-muted-foreground">Avatar</Label>
        <div className="mt-1.5 flex items-center gap-4">
          <button
            type="button"
            className="group relative h-16 w-16 shrink-0 rounded-full bg-muted overflow-hidden focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploading}
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size={64} className="rounded-none" />
            <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
              {uploading ? (
                <Loader2 className="h-5 w-5 animate-spin text-white" />
              ) : (
                <Camera className="h-5 w-5 text-white" />
              )}
            </div>
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={handleAvatarUpload}
          />
          <div className="text-xs text-muted-foreground">
            Click to upload avatar
          </div>
        </div>
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Name</Label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Description</Label>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What does this agent do?"
          className="mt-1"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Visibility</Label>
        <div className="mt-1.5 flex gap-2">
          <button
            type="button"
            onClick={() => setVisibility("workspace")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              visibility === "workspace"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="text-left">
              <div className="font-medium">Workspace</div>
              <div className="text-xs text-muted-foreground">All members can assign</div>
            </div>
          </button>
          <button
            type="button"
            onClick={() => setVisibility("private")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              visibility === "private"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="text-left">
              <div className="font-medium">Private</div>
              <div className="text-xs text-muted-foreground">Only you can assign</div>
            </div>
          </button>
        </div>
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Max Concurrent Tasks</Label>
        <Input
          type="number"
          min={1}
          max={50}
          value={maxTasks}
          onChange={(e) => setMaxTasks(Number(e.target.value))}
          className="mt-1 w-24"
        />
      </div>

      {(runtimeProvider === "claude" || runtimeProvider === "codex" || runtimeProvider === "opencode") && (
        <div>
          <Label className="text-xs text-muted-foreground">Config Mode</Label>
          <div className="mt-1.5 flex gap-2">
            <button
              type="button"
              onClick={() => setConfigMode("global")}
              className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                configMode === "global"
                  ? "border-primary bg-primary/5"
                  : "border-border hover:bg-muted"
              }`}
            >
              <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
              <div className="text-left">
                <div className="font-medium">Global</div>
                <div className="text-xs text-muted-foreground">
                  {runtimeProvider === "codex" ? "Use system-wide Codex config" : runtimeProvider === "opencode" ? "Use system-wide OpenCode config" : "Use system-wide Claude config"}
                </div>
              </div>
            </button>
            <button
              type="button"
              onClick={() => setConfigMode("project")}
              className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                configMode === "project"
                  ? "border-primary bg-primary/5"
                  : "border-border hover:bg-muted"
              }`}
            >
              <Settings className="h-4 w-4 shrink-0 text-muted-foreground" />
              <div className="text-left">
                <div className="font-medium">Project</div>
                <div className="text-xs text-muted-foreground">
                  {runtimeProvider === "codex" ? "Per-task config.toml" : runtimeProvider === "opencode" ? "Per-task opencode.json" : "Per-task .claude/settings.json"}
                </div>
              </div>
            </button>
          </div>
        </div>
      )}

      {((runtimeProvider !== "claude" && runtimeProvider !== "codex" && runtimeProvider !== "opencode") || (runtimeProvider === "codex" && configMode === "project")) && (
      <div>
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-xs text-muted-foreground">Environment Variables</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Custom environment variables injected into the agent&apos;s execution environment.
            </p>
          </div>
          <div className="flex items-center gap-1.5">
            <div className="flex rounded-md border">
              <button
                type="button"
                onClick={() => envMode === "json" ? switchToList() : undefined}
                className={`flex items-center gap-1 px-2 py-1 text-xs transition-colors ${
                  envMode === "list"
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <List className="h-3 w-3" />
                List
              </button>
              <button
                type="button"
                onClick={() => envMode === "list" ? switchToJson() : undefined}
                className={`flex items-center gap-1 px-2 py-1 text-xs transition-colors ${
                  envMode === "json"
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <Braces className="h-3 w-3" />
                JSON
              </button>
            </div>
            {envMode === "list" && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEnvVars([...envVars, { key: "", value: "" }])}
              >
                <Plus className="h-3 w-3 mr-1" />
                Add
              </Button>
            )}
          </div>
        </div>
        {envMode === "list" ? (
          envVars.length === 0 ? (
            <div className="mt-2 rounded-lg border border-dashed py-6 text-center">
              <p className="text-xs text-muted-foreground">No custom environment variables</p>
            </div>
          ) : (
            <div className="mt-2 space-y-2">
              {envVars.map((ev, index) => (
                <div key={index} className="flex items-center gap-2">
                  <Input
                    type="text"
                    value={ev.key}
                    onChange={(e) => {
                      const next = [...envVars];
                      const current = next[index]!;
                      next[index] = { key: e.target.value, value: current.value };
                      setEnvVars(next);
                    }}
                    placeholder="KEY"
                    className="flex-1 font-mono text-xs"
                  />
                  <span className="text-muted-foreground">=</span>
                  <Input
                    type="text"
                    value={ev.value}
                    onChange={(e) => {
                      const next = [...envVars];
                      const current = next[index]!;
                      next[index] = { key: current.key, value: e.target.value };
                      setEnvVars(next);
                    }}
                    placeholder="value"
                    className="flex-1 font-mono text-xs"
                  />
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => setEnvVars(envVars.filter((_, i) => i !== index))}
                    className="text-muted-foreground hover:text-destructive shrink-0"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )
        ) : (
          <div className="mt-2">
            <Textarea
              ref={jsonTextareaRef}
              value={jsonText}
              onChange={(e) => {
                setJsonText(e.target.value);
                setJsonError(null);
              }}
              placeholder={'{\n  "API_KEY": "your-key",\n  "DB_HOST": "localhost"\n}'}
              className="min-h-[120px] font-mono text-xs resize-none overflow-hidden"
              spellCheck={false}
            />
            {jsonError && (
              <p className="mt-1 text-xs text-destructive">{jsonError}</p>
            )}
          </div>
        )}
      </div>
      )}

      {runtimeProvider === "claude" && configMode === "project" && (
      <div>
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-xs text-muted-foreground">settings.json</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Written to <code className="rounded bg-muted px-1 py-0.5 text-xs">.claude/settings.json</code> in each task&apos;s workdir. Paste your CC Switch config directly.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => setClaudeDocsOpen(true)}
          >
            <FileText className="h-3.5 w-3.5 mr-1" />
            Config Reference
          </Button>
        </div>
        <div className="mt-2">
          <Textarea
            ref={claudeSettingsRef}
            value={claudeSettingsJson}
            onChange={(e) => setClaudeSettingsJson(e.target.value)}
            placeholder={'{\n  "env": {\n    "ANTHROPIC_AUTH_TOKEN": "sk-xxx",\n    "ANTHROPIC_BASE_URL": "http://xxx:8080",\n    "ANTHROPIC_MODEL": "glm-5.1"\n  },\n  "model": "opus[1m]",\n  "skipDangerousModePermissionPrompt": true\n}'}
            className="min-h-[200px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
            spellCheck={false}
          />
        </div>

        <Dialog open={claudeDocsOpen} onOpenChange={setClaudeDocsOpen}>
          <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
            <DialogHeader>
              <DialogTitle>settings.json Reference</DialogTitle>
              <DialogDescription>
                Configuration for Claude Code. This file is written to <code className="rounded bg-muted px-1 py-0.5 text-xs">.claude/settings.json</code> in the task&apos;s workdir. You can paste your CC Switch configuration directly.
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
              <section>
                <h4 className="font-medium mb-1.5">Example Configuration</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  A complete example that you can paste directly from CC Switch.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-xxx",
    "ANTHROPIC_BASE_URL": "http://xxx:8080",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-5.1",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "glm-5.1",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
    "ANTHROPIC_MODEL": "glm-5.1",
    "ANTHROPIC_REASONING_MODEL": "glm-5.1",
    "DISABLE_BUG_COMMAND": "1",
    "DISABLE_ERROR_REPORTING": "1",
    "DISABLE_TELEMETRY": "1"
  },
  "model": "opus[1m]",
  "skipDangerousModePermissionPrompt": true,
  "statusLine": {
    "command": "npx -y ccstatusline@latest",
    "padding": 0,
    "type": "command"
  }
}`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Environment Variables</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  API keys, model overrides, and other settings passed via the <code className="rounded bg-muted px-1 py-0.5 text-xs">env</code> object.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"env": {
  "ANTHROPIC_AUTH_TOKEN": "sk-xxx",        // API key
  "ANTHROPIC_BASE_URL": "http://xxx:8080", // Custom API endpoint
  "ANTHROPIC_MODEL": "glm-5.1",            // Default model
  "ANTHROPIC_REASONING_MODEL": "glm-5.1",  // Reasoning model
  "DISABLE_TELEMETRY": "1"                 // Disable telemetry
}`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Model & Permissions</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Control the model selection and permission prompts.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"model": "opus[1m]",                         // Model with context window
"skipDangerousModePermissionPrompt": true      // Skip dangerous mode prompt`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Status Line</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Display a custom status line in the terminal.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"statusLine": {
  "command": "npx -y ccstatusline@latest",
  "padding": 0,
  "type": "command"
}`}</pre>
              </section>

              <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                <strong className="text-foreground">Tip:</strong> You can copy your CC Switch configuration and paste it directly. Changes take effect on the next task execution. Config is written per-task to an isolated workdir.
              </div>
            </div>
          </DialogContent>
        </Dialog>
      </div>
      )}

      {runtimeProvider === "codex" && configMode === "project" && (
      <div>
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-xs text-muted-foreground">config.toml</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Written to each task&apos;s CODEX_HOME, overrides system defaults.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => setCodexDocsOpen(true)}
          >
            <FileText className="h-3.5 w-3.5 mr-1" />
            Config Reference
          </Button>
        </div>
        <div className="mt-2">
          <Textarea
            ref={codexTomlRef}
            value={codexConfigToml}
            onChange={(e) => setCodexConfigToml(e.target.value)}
            placeholder={'# Example config.toml\nmodel = "gpt-5.2-codex"\nsandbox_mode = "workspace-write"\napproval_policy = "ask"'}
            className="min-h-[160px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
            spellCheck={false}
          />
        </div>

        <Dialog open={codexDocsOpen} onOpenChange={setCodexDocsOpen}>
          <DialogContent className="sm:max-w-5xl max-h-[85vh] flex flex-col">
            <DialogHeader>
              <DialogTitle>config.toml Reference</DialogTitle>
              <DialogDescription>
                Configuration options for Codex. This file is written to the task&apos;s CODEX_HOME directory.
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
              <section>
                <h4 className="font-medium mb-1.5">Model Configuration</h4>
                <p className="text-xs text-muted-foreground mb-2">Specify which model Codex should use.</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`model = "gpt-5.2-codex"           # Model to use
model_provider = "openai"          # Provider name
model_context_window = 128000      # Context window (tokens)
model_reasoning_effort = "medium"  # "low" | "medium" | "high"`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Custom Model Provider</h4>
                <p className="text-xs text-muted-foreground mb-2">Connect to third-party or local model providers (LM Studio, Ollama, etc.).</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`[model_providers.my-provider]
name = "My Provider"
base_url = "https://api.example.com/v1"
env_key = "MY_API_KEY"            # Env var holding the API key
wire_api = "responses"            # "responses" | "chat"`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Sandbox & Approval</h4>
                <p className="text-xs text-muted-foreground mb-2">Control what Codex is allowed to do.</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`sandbox_mode = "workspace-write"  # "read-only" | "workspace-write" | "danger-full-access"
approval_policy = "ask"           # "never" | "ask" | "always"`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">MCP Servers</h4>
                <p className="text-xs text-muted-foreground mb-2">Add external tool servers that Codex can call.</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`[mcp_servers.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
enabled = true
tool_timeout_sec = 60

# HTTP-based MCP server
[mcp_servers.github]
transport = "http"
url = "http://localhost:3001/mcp"
bearer_token = "your-token"
enabled = true`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Feature Toggles</h4>
                <p className="text-xs text-muted-foreground mb-2">Enable or disable specific Codex capabilities.</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`[features]
web_search = true
js_repl = true
python_repl = true
shell = true`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Environment Variables</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  API keys and secrets should <strong>not</strong> be stored in config.toml.
                  Instead, add them in the <strong>Environment Variables</strong> section above,
                  then reference them via <code className="rounded bg-muted px-1 py-0.5 text-xs">env_key</code> in your provider config.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`# In config.toml — just name the env var:
[model_providers.my-provider]
env_key = "MY_API_KEY"

# In Environment Variables above — set the actual value:
# MY_API_KEY = sk-xxxxxxxx`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Profiles</h4>
                <p className="text-xs text-muted-foreground mb-2">Define named configuration profiles for different environments.</p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`default_profile = "development"

[profiles.development]
model = "gpt-5.2-codex"
sandbox_mode = "workspace-write"

[profiles.production]
model = "gpt-5.2-codex"
sandbox_mode = "read-only"
approval_policy = "always"`}</pre>
              </section>

              <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                <strong className="text-foreground">Tip:</strong> Changes take effect on the next task execution. Config is written per-task to an isolated CODEX_HOME directory.
              </div>
            </div>
          </DialogContent>
        </Dialog>
      </div>
      )}

      {runtimeProvider === "opencode" && configMode === "project" && (
      <div>
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-xs text-muted-foreground">opencode.json</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Written to <code className="rounded bg-muted px-1 py-0.5 text-xs">opencode.json</code> in each task&apos;s workdir.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-muted-foreground hover:text-foreground"
            onClick={() => setOpencodeDocsOpen(true)}
          >
            <FileText className="h-3.5 w-3.5 mr-1" />
            Config Reference
          </Button>
        </div>
        <div className="mt-2">
          <Textarea
            ref={opencodeConfigRef}
            value={opencodeConfigJson}
            onChange={(e) => setOpencodeConfigJson(e.target.value)}
            placeholder={'{\n  "$schema": "https://opencode.ai/config.json",\n  "model": "provider/model-id",\n  "provider": {\n    "myprovider": {\n      "npm": "@ai-sdk/openai-compatible",\n      "name": "My Provider",\n      "options": {\n        "baseURL": "https://api.example.com/v1",\n        "apiKey": "sk-xxx"\n      },\n      "models": {\n        "model-id": {\n          "name": "Model Name"\n        }\n      }\n    }\n  }\n}'}
            className="min-h-[200px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
            spellCheck={false}
          />
        </div>

        <Dialog open={opencodeDocsOpen} onOpenChange={setOpencodeDocsOpen}>
          <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
            <DialogHeader>
              <DialogTitle>opencode.json Reference</DialogTitle>
              <DialogDescription>
                Configuration for OpenCode. This file is written to <code className="rounded bg-muted px-1 py-0.5 text-xs">opencode.json</code> in the task&apos;s workdir. Project config overrides global defaults.
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
              <section>
                <h4 className="font-medium mb-1.5">Example Configuration</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  A complete example showing model, provider, and permission settings.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5",
  "provider": {
    "openai": {
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "sk-xxx"
      }
    }
  },
  "permission": {
    "edit": {
      "src/generated/*": "deny"
    }
  }
}`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Model Configuration</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Specify which models OpenCode should use.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"model": "anthropic/claude-sonnet-4-5",       // Main model
"small_model": "anthropic/claude-haiku-4-5"  // Lightweight tasks`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Provider Configuration</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Configure providers with custom endpoints, timeouts, and API keys. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">npm</code> to register third-party AI SDK packages.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"provider": {
  "anthropic": {
    "options": {
      "timeout": 600000,
      "chunkTimeout": 30000
    }
  },
  "openai": {
    "options": {
      "baseURL": "http://localhost:8080/v1",
      "apiKey": "sk-xxx"
    }
  },
  "custom": {
    "npm": "@ai-sdk/openai-compatible",
    "name": "My Provider",
    "options": {
      "baseURL": "https://api.example.com/v1",
      "apiKey": "sk-xxx"
    },
    "models": {
      "my-model": {
        "name": "My Model"
      }
    }
  }
}`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Permissions</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Control which files can be edited. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">"deny"</code> to block specific paths.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"permission": {
  "edit": {
    "packages/opencode/migration/*": "deny",
    "src/generated/*": "deny"
  }
}`}</pre>
              </section>

              <section>
                <h4 className="font-medium mb-1.5">Tools & MCP</h4>
                <p className="text-xs text-muted-foreground mb-2">
                  Enable or disable specific tools, or add MCP servers.
                </p>
                <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"tools": {
  "github-triage": false,
  "github-pr-search": false
},
"mcp": {}`}</pre>
              </section>

              <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                <strong className="text-foreground">Tip:</strong> Config is written per-task to an isolated workdir. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">$schema</code> for editor auto-completion.
              </div>
            </div>
          </DialogContent>
        </Dialog>
      </div>
      )}

      <div>
        <Label className="text-xs text-muted-foreground">Runtime</Label>
        <div className="mt-1 flex items-center gap-2 rounded-lg border px-3 py-2.5 text-sm text-muted-foreground">
          {agent.runtime_mode === "cloud" ? (
            <Cloud className="h-4 w-4" />
          ) : (
            <Monitor className="h-4 w-4" />
          )}
          {runtimeDevice?.name ?? (agent.runtime_mode === "cloud" ? "Cloud" : "Local")}
        </div>
      </div>

      <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
        {saving ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <Save className="h-3.5 w-3.5 mr-1.5" />}
        Save Changes
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Agent Detail
// ---------------------------------------------------------------------------

type DetailTab = "instructions" | "skills" | "tasks" | "settings";

const detailTabs: { id: DetailTab; label: string; icon: typeof FileText }[] = [
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "tasks", label: "Tasks", icon: ListTodo },
  { id: "settings", label: "Settings", icon: Settings },
];

function AgentDetail({
  agent,
  runtimes,
  onUpdate,
  onArchive,
  onRestore,
}: {
  agent: Agent;
  runtimes: RuntimeDevice[];
  onUpdate: (id: string, data: Partial<Agent>) => Promise<void>;
  onArchive: (id: string) => Promise<void>;
  onRestore: (id: string) => Promise<void>;
}) {
  const st = statusConfig[agent.status];
  const runtimeDevice = getRuntimeDevice(agent, runtimes);
  const [activeTab, setActiveTab] = useState<DetailTab>("instructions");
  const [confirmArchive, setConfirmArchive] = useState(false);
  const isArchived = !!agent.archived_at;

  return (
    <div className="flex h-full flex-col">
      {/* Archive Banner */}
      {isArchived && (
        <div className="flex items-center gap-2 bg-muted/50 px-4 py-2 text-xs text-muted-foreground border-b">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span className="flex-1">This agent is archived. It cannot be assigned or mentioned.</span>
          <Button variant="outline" size="sm" className="h-6 text-xs" onClick={() => onRestore(agent.id)}>
            Restore
          </Button>
        </div>
      )}

      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <ActorAvatar actorType="agent" actorId={agent.id} size={28} className={`rounded-md ${isArchived ? "opacity-50" : ""}`} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className={`text-sm font-semibold truncate ${isArchived ? "text-muted-foreground" : ""}`}>{agent.name}</h2>
            {isArchived ? (
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
                Archived
              </span>
            ) : (
              <span className={`flex items-center gap-1.5 text-xs ${st.color}`}>
                <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
                {st.label}
              </span>
            )}
            <span className="flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
              {agent.runtime_mode === "cloud" ? (
                <Cloud className="h-3 w-3" />
              ) : (
                <Monitor className="h-3 w-3" />
              )}
              {runtimeDevice?.name ?? (agent.runtime_mode === "cloud" ? "Cloud" : "Local")}
            </span>
          </div>
        </div>
        {!isArchived && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" />
              }
            >
              <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuItem
                className="text-destructive"
                onClick={() => setConfirmArchive(true)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Archive Agent
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>

      {/* Tabs */}
      <div className="flex border-b px-6">
        {detailTabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`flex items-center gap-1.5 border-b-2 px-3 py-2.5 text-xs font-medium transition-colors ${
              activeTab === tab.id
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <tab.icon className="h-3.5 w-3.5" />
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {activeTab === "instructions" && (
          <InstructionsTab
            agent={agent}
            onSave={(instructions) => onUpdate(agent.id, { instructions })}
          />
        )}
        {activeTab === "skills" && (
          <SkillsTab agent={agent} />
        )}
        {activeTab === "tasks" && <TasksTab agent={agent} />}
        {activeTab === "settings" && (
          <SettingsTab
            agent={agent}
            runtimes={runtimes}
            onSave={(updates) => onUpdate(agent.id, updates)}
          />
        )}
      </div>

      {/* Archive Confirmation */}
      {confirmArchive && (
        <Dialog open onOpenChange={(v) => { if (!v) setConfirmArchive(false); }}>
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                <AlertCircle className="h-5 w-5 text-destructive" />
              </div>
              <DialogHeader className="flex-1 gap-1">
                <DialogTitle className="text-sm font-semibold">Archive agent?</DialogTitle>
                <DialogDescription className="text-xs">
                  &quot;{agent.name}&quot; will be archived. It won&apos;t be assignable or mentionable, but all history is preserved. You can restore it later.
                </DialogDescription>
              </DialogHeader>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setConfirmArchive(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  onArchive(agent.id);
                }}
              >
                Archive
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function AgentsPage() {
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [selectedId, setSelectedId] = useState<string>("");
  const [showArchived, setShowArchived] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agents_layout",
  });

  const filteredAgents = useMemo(
    () => showArchived ? agents.filter((a) => !!a.archived_at) : agents.filter((a) => !a.archived_at),
    [agents, showArchived],
  );

  const archivedCount = useMemo(() => agents.filter((a) => !!a.archived_at).length, [agents]);

  // Select first agent on initial load or when filter changes
  useEffect(() => {
    if (filteredAgents.length > 0 && !filteredAgents.some((a) => a.id === selectedId)) {
      setSelectedId(filteredAgents[0]!.id);
    }
  }, [filteredAgents, selectedId]);

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    setSelectedId(agent.id);
  };

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent");
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to archive agent");
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to restore agent");
    }
  };

  const selected = agents.find((a) => a.id === selectedId) ?? null;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0">
        {/* List skeleton */}
        <div className="w-72 border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-6 w-6 rounded" />
          </div>
          <div className="divide-y">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-24" />
                  <Skeleton className="h-3 w-16" />
                </div>
              </div>
            ))}
          </div>
        </div>
        {/* Detail skeleton */}
        <div className="flex-1 p-6 space-y-6">
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-10 rounded-full" />
            <div className="space-y-1.5">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3 w-20" />
            </div>
          </div>
          <div className="space-y-3">
            <Skeleton className="h-8 w-full rounded-lg" />
            <Skeleton className="h-8 w-full rounded-lg" />
            <Skeleton className="h-8 w-3/4 rounded-lg" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="list" defaultSize={280} minSize={240} maxSize={400} groupResizeBehavior="preserve-pixel-size">
        {/* Left column — agent list */}
        <div className="overflow-y-auto h-full border-r">
          <div className="flex h-12 items-center justify-between border-b px-4">
            <h1 className="text-sm font-semibold">Agents</h1>
            <div className="flex items-center gap-1">
              {archivedCount > 0 && (
                <Button
                  variant={showArchived ? "secondary" : "ghost"}
                  size="icon-xs"
                  onClick={() => setShowArchived(!showArchived)}
                  title={showArchived ? "Show active agents" : "Show archived agents"}
                >
                  <Archive className="h-4 w-4 text-muted-foreground" />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setShowCreate(true)}
              >
                <Plus className="h-4 w-4 text-muted-foreground" />
              </Button>
            </div>
          </div>
          {filteredAgents.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Bot className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">
                {showArchived ? "No archived agents" : archivedCount > 0 ? "No active agents" : "No agents yet"}
              </p>
              {!showArchived && (
                <Button
                  onClick={() => setShowCreate(true)}
                  size="xs"
                  className="mt-3"
                >
                  <Plus className="h-3 w-3" />
                  Create Agent
                </Button>
              )}
            </div>
          ) : (
            <div className="divide-y">
              {filteredAgents.map((agent) => (
                <AgentListItem
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === selectedId}
                  onClick={() => setSelectedId(agent.id)}
                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {/* Right column — agent detail */}
        {selected ? (
          <AgentDetail
            key={selected.id}
            agent={selected}
            runtimes={runtimes}
            onUpdate={handleUpdate}
            onArchive={handleArchive}
            onRestore={handleRestore}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
            <Bot className="h-10 w-10 text-muted-foreground/30" />
            <p className="mt-3 text-sm">Select an agent to view details</p>
            <Button
              onClick={() => setShowCreate(true)}
              size="xs"
              className="mt-3"
            >
              <Plus className="h-3 w-3" />
              Create Agent
            </Button>
          </div>
        )}
      </ResizablePanel>

      {showCreate && (
        <CreateAgentDialog
          runtimes={runtimes}
          onClose={() => setShowCreate(false)}
          onCreate={handleCreate}
        />
      )}
    </ResizablePanelGroup>
  );
}

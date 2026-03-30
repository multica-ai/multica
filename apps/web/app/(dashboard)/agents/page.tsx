"use client";

import { useState, useEffect } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import {
  Bot,
  Cloud,
  Monitor,
  Plus,
  ListTodo,
  Wrench,
  FileText,
  BookOpenText,
  Timer,
  Trash2,
  Save,
  Key,
  Link2,
  Clock,
  CheckCircle2,
  XCircle,
  Loader2,
  AlertCircle,
  MoreHorizontal,
  Play,
  ChevronDown,
} from "lucide-react";
import type {
  Agent,
  AgentStatus,
  AgentTool,
  AgentTrigger,
  AgentTask,
  RuntimeDevice,
  CreateAgentRequest,
  UpdateAgentRequest,
} from "@/shared/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { useRuntimeStore } from "@/features/runtimes";
import { useIssueStore } from "@/features/issues";


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

function getInitials(name: string): string {
  return name
    .split(/[\s-]+/)
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

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
        triggers: [{ id: generateId(), type: "on_assign", enabled: true, config: {} }],
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

  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted text-xs font-semibold">
        {getInitials(agent.name)}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{agent.name}</span>
          {agent.runtime_mode === "cloud" ? (
            <Cloud className="h-3 w-3 text-muted-foreground" />
          ) : (
            <Monitor className="h-3 w-3 text-muted-foreground" />
          )}
        </div>
        <div className="flex items-center gap-1.5 mt-0.5">
          <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
          <span className={`text-xs ${st.color}`}>{st.label}</span>
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
  const workspaceSkills = useWorkspaceStore((s) => s.skills);
  const refreshAgents = useWorkspaceStore((s) => s.refreshAgents);
  const [saving, setSaving] = useState(false);
  const [showPicker, setShowPicker] = useState(false);

  const agentSkillIds = new Set(agent.skills.map((s) => s.id));
  const availableSkills = workspaceSkills.filter((s) => !agentSkillIds.has(s.id));

  const handleAdd = async (skillId: string) => {
    setSaving(true);
    try {
      const newIds = [...agent.skills.map((s) => s.id), skillId];
      await api.setAgentSkills(agent.id, { skill_ids: newIds });
      await refreshAgents();
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
      await refreshAgents();
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
// Tools Tab
// ---------------------------------------------------------------------------

function AddToolDialog({
  onClose,
  onAdd,
}: {
  onClose: () => void;
  onAdd: (tool: AgentTool) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [authType, setAuthType] = useState<"oauth" | "api_key" | "none">("api_key");

  const handleAdd = () => {
    if (!name.trim()) return;
    onAdd({
      id: generateId(),
      name: name.trim(),
      description: description.trim(),
      auth_type: authType,
      connected: false,
      config: {},
    });
    onClose();
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="text-sm">Add Tool</DialogTitle>
          <DialogDescription className="text-xs">
            Connect an external tool for this agent to use.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div>
            <Label className="text-xs text-muted-foreground">Tool Name</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Google Search, Slack, GitHub"
              className="mt-1"
              onKeyDown={(e) => e.key === "Enter" && handleAdd()}
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this tool do?"
              className="mt-1"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Authentication</Label>
            <div className="mt-1.5 flex gap-2">
              {(["api_key", "oauth", "none"] as const).map((type) => (
                <Button
                  key={type}
                  variant={authType === type ? "outline" : "ghost"}
                  size="xs"
                  onClick={() => setAuthType(type)}
                  className={`flex-1 ${
                    authType === type
                      ? "border-primary bg-primary/5 font-medium"
                      : ""
                  }`}
                >
                  {type === "api_key" ? "API Key" : type === "oauth" ? "OAuth" : "None"}
                </Button>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleAdd}
            disabled={!name.trim()}
          >
            Add
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ToolsTab({
  agent,
  onSave,
}: {
  agent: Agent;
  onSave: (tools: AgentTool[]) => Promise<void>;
}) {
  const [tools, setTools] = useState<AgentTool[]>(agent.tools ?? []);
  const [showAdd, setShowAdd] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setTools(agent.tools ?? []);
  }, [agent.id, agent.tools]);

  const isDirty = JSON.stringify(tools) !== JSON.stringify(agent.tools ?? []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(tools);
    } finally {
      setSaving(false);
    }
  };

  const toggleConnect = (toolId: string) => {
    setTools((prev) =>
      prev.map((t) => (t.id === toolId ? { ...t, connected: !t.connected } : t)),
    );
  };

  const removeTool = (toolId: string) => {
    setTools((prev) => prev.filter((t) => t.id !== toolId));
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">Tools</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            External tools and APIs this agent can use during task execution.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isDirty && (
            <Button
              onClick={handleSave}
              disabled={saving}
              size="xs"
            >
              <Save className="h-3 w-3" />
              {saving ? "Saving..." : "Save"}
            </Button>
          )}
          <Button
            variant="outline"
            size="xs"
            onClick={() => setShowAdd(true)}
          >
            <Plus className="h-3 w-3" />
            Add Tool
          </Button>
        </div>
      </div>

      {tools.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <Wrench className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No tools configured</p>
          <Button
            onClick={() => setShowAdd(true)}
            size="xs"
            className="mt-3"
          >
            <Plus className="h-3 w-3" />
            Add Tool
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          {tools.map((tool) => (
            <div
              key={tool.id}
              className="flex items-center gap-3 rounded-lg border px-4 py-3"
            >
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                {tool.auth_type === "oauth" ? (
                  <Link2 className="h-4 w-4 text-muted-foreground" />
                ) : tool.auth_type === "api_key" ? (
                  <Key className="h-4 w-4 text-muted-foreground" />
                ) : (
                  <Wrench className="h-4 w-4 text-muted-foreground" />
                )}
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{tool.name}</div>
                {tool.description && (
                  <div className="text-xs text-muted-foreground truncate">
                    {tool.description}
                  </div>
                )}
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => toggleConnect(tool.id)}
                  className={
                    tool.connected
                      ? "bg-success/10 text-success"
                      : "bg-muted text-muted-foreground hover:bg-accent"
                  }
                >
                  {tool.connected ? "Connected" : "Connect"}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => removeTool(tool.id)}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showAdd && (
        <AddToolDialog
          onClose={() => setShowAdd(false)}
          onAdd={(tool) => setTools((prev) => [...prev, tool])}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Triggers Tab
// ---------------------------------------------------------------------------

function TriggersTab({
  agent,
  onSave,
}: {
  agent: Agent;
  onSave: (triggers: AgentTrigger[]) => Promise<void>;
}) {
  const [triggers, setTriggers] = useState<AgentTrigger[]>(agent.triggers ?? []);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setTriggers(agent.triggers ?? []);
  }, [agent.id, agent.triggers]);

  const isDirty = JSON.stringify(triggers) !== JSON.stringify(agent.triggers ?? []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(triggers);
    } finally {
      setSaving(false);
    }
  };

  const toggleTrigger = (triggerId: string) => {
    setTriggers((prev) =>
      prev.map((t) => (t.id === triggerId ? { ...t, enabled: !t.enabled } : t)),
    );
  };

  const removeTrigger = (triggerId: string) => {
    setTriggers((prev) => prev.filter((t) => t.id !== triggerId));
  };

  const addTrigger = (type: "on_assign" | "scheduled") => {
    const newTrigger: AgentTrigger = {
      id: generateId(),
      type,
      enabled: true,
      config: type === "scheduled" ? { cron: "0 9 * * 1-5", timezone: "UTC" } : {},
    };
    setTriggers((prev) => [...prev, newTrigger]);
  };

  const updateTriggerConfig = (triggerId: string, config: Record<string, unknown>) => {
    setTriggers((prev) =>
      prev.map((t) => (t.id === triggerId ? { ...t, config } : t)),
    );
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold">Triggers</h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            Configure when this agent should start working.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isDirty && (
            <Button
              onClick={handleSave}
              disabled={saving}
              size="xs"
            >
              <Save className="h-3 w-3" />
              {saving ? "Saving..." : "Save"}
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-2">
        {triggers.map((trigger) => (
          <div
            key={trigger.id}
            className="rounded-lg border px-4 py-3"
          >
            <div className="flex items-center gap-3">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                {trigger.type === "on_assign" ? (
                  <Bot className="h-4 w-4 text-muted-foreground" />
                ) : (
                  <Timer className="h-4 w-4 text-muted-foreground" />
                )}
              </div>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">
                  {trigger.type === "on_assign" ? "On Issue Assign" : "Scheduled"}
                </div>
                <div className="text-xs text-muted-foreground">
                  {trigger.type === "on_assign"
                    ? "Runs when an issue is assigned to this agent"
                    : `Cron: ${(trigger.config as { cron?: string }).cron ?? "Not set"}`}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => toggleTrigger(trigger.id)}
                  className={`relative h-5 w-9 rounded-full transition-colors ${
                    trigger.enabled ? "bg-primary" : "bg-muted"
                  }`}
                >
                  <span
                    className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${
                      trigger.enabled ? "left-4.5" : "left-0.5"
                    }`}
                  />
                </button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => removeTrigger(trigger.id)}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>

            {trigger.type === "scheduled" && (
              <div className="mt-3 grid grid-cols-2 gap-3 pl-12">
                <div>
                  <Label className="text-xs text-muted-foreground">
                    Cron Expression
                  </Label>
                  <Input
                    type="text"
                    value={(trigger.config as { cron?: string }).cron ?? ""}
                    onChange={(e) =>
                      updateTriggerConfig(trigger.id, {
                        ...trigger.config,
                        cron: e.target.value,
                      })
                    }
                    placeholder="0 9 * * 1-5"
                    className="mt-1 text-xs font-mono"
                  />
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">
                    Timezone
                  </Label>
                  <Input
                    type="text"
                    value={(trigger.config as { timezone?: string }).timezone ?? ""}
                    onChange={(e) =>
                      updateTriggerConfig(trigger.id, {
                        ...trigger.config,
                        timezone: e.target.value,
                      })
                    }
                    placeholder="UTC"
                    className="mt-1 text-xs"
                  />
                </div>
              </div>
            )}
          </div>
        ))}
      </div>

      <div className="flex gap-2">
        <Button
          variant="outline"
          size="xs"
          onClick={() => addTrigger("on_assign")}
          className="border-dashed text-muted-foreground hover:text-foreground"
        >
          <Bot className="h-3 w-3" />
          Add On Assign
        </Button>
        <Button
          variant="outline"
          size="xs"
          onClick={() => addTrigger("scheduled")}
          className="border-dashed text-muted-foreground hover:text-foreground"
        >
          <Timer className="h-3 w-3" />
          Add Scheduled
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tasks Tab
// ---------------------------------------------------------------------------

function TasksTab({ agent }: { agent: Agent }) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [loading, setLoading] = useState(true);
  const issues = useIssueStore((s) => s.issues);

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
      <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
        Loading tasks...
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
// Agent Detail
// ---------------------------------------------------------------------------

type DetailTab = "instructions" | "skills" | "tools" | "triggers" | "tasks";

const detailTabs: { id: DetailTab; label: string; icon: typeof FileText }[] = [
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "triggers", label: "Triggers", icon: Timer },
  { id: "tasks", label: "Tasks", icon: ListTodo },
];

function AgentDetail({
  agent,
  runtimes,
  onUpdate,
  onDelete,
}: {
  agent: Agent;
  runtimes: RuntimeDevice[];
  onUpdate: (id: string, data: Partial<Agent>) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}) {
  const st = statusConfig[agent.status];
  const runtimeDevice = getRuntimeDevice(agent, runtimes);
  const [activeTab, setActiveTab] = useState<DetailTab>("instructions");
  const [confirmDelete, setConfirmDelete] = useState(false);

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted text-xs font-bold">
          {getInitials(agent.name)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold truncate">{agent.name}</h2>
            <span className={`flex items-center gap-1.5 text-xs ${st.color}`}>
              <span className={`h-1.5 w-1.5 rounded-full ${st.dot}`} />
              {st.label}
            </span>
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
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" />
            }
          >
            <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              className="text-destructive"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete Agent
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
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
        {activeTab === "tools" && (
          <ToolsTab
            agent={agent}
            onSave={(tools) => onUpdate(agent.id, { tools })}
          />
        )}
        {activeTab === "triggers" && (
          <TriggersTab
            agent={agent}
            onSave={(triggers) => onUpdate(agent.id, { triggers })}
          />
        )}
        {activeTab === "tasks" && <TasksTab agent={agent} />}
      </div>

      {/* Delete Confirmation */}
      {confirmDelete && (
        <Dialog open onOpenChange={(v) => { if (!v) setConfirmDelete(false); }}>
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                <AlertCircle className="h-5 w-5 text-destructive" />
              </div>
              <DialogHeader className="flex-1 gap-1">
                <DialogTitle className="text-sm font-semibold">Delete agent?</DialogTitle>
                <DialogDescription className="text-xs">
                  This will permanently delete &quot;{agent.name}&quot; and all its configuration.
                </DialogDescription>
              </DialogHeader>
            </div>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmDelete(false);
                  onDelete(agent.id);
                }}
              >
                Delete
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
  const agents = useWorkspaceStore((s) => s.agents);
  const refreshAgents = useWorkspaceStore((s) => s.refreshAgents);
  const [selectedId, setSelectedId] = useState<string>("");
  const [showCreate, setShowCreate] = useState(false);
  const runtimes = useRuntimeStore((s) => s.runtimes);
  const fetchRuntimes = useRuntimeStore((s) => s.fetchRuntimes);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agents_layout",
  });

  useEffect(() => {
    if (workspace) fetchRuntimes();
  }, [workspace, fetchRuntimes]);

  // Select first agent on initial load
  useEffect(() => {
    if (agents.length > 0 && !selectedId) {
      setSelectedId(agents[0]!.id);
    }
  }, [agents, selectedId]);

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    await refreshAgents();
    setSelectedId(agent.id);
  };

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    await api.updateAgent(id, data as UpdateAgentRequest);
    await refreshAgents();
  };

  const handleDelete = async (id: string) => {
    await api.deleteAgent(id);
    if (selectedId === id) {
      const remaining = agents.filter((a) => a.id !== id);
      setSelectedId(remaining[0]?.id ?? "");
    }
    await refreshAgents();
  };

  const selected = agents.find((a) => a.id === selectedId) ?? null;

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
        Loading...
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
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => setShowCreate(true)}
            >
              <Plus className="h-4 w-4 text-muted-foreground" />
            </Button>
          </div>
          {agents.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Bot className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">No agents yet</p>
              <Button
                onClick={() => setShowCreate(true)}
                size="xs"
                className="mt-3"
              >
                <Plus className="h-3 w-3" />
                Create Agent
              </Button>
            </div>
          ) : (
            <div className="divide-y">
              {agents.map((agent) => (
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
            agent={selected}
            runtimes={runtimes}
            onUpdate={handleUpdate}
            onDelete={handleDelete}
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

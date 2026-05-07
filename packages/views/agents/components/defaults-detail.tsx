"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Loader2,
  Save,
  Plus,
  Trash2,
  Eye,
  EyeOff,
  FileText,
  KeyRound,
  Terminal,
  BookOpenText,
  Settings2,
  Sliders,
  Copy,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import type { AgentDefaultsWithUser } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";

interface AgentDefaultsConfig {
  instructions?: string;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  skills?: string[];
}

let nextEnvId = 0;
interface EnvEntry {
  id: number;
  key: string;
  value: string;
  visible: boolean;
}

function envMapToEntries(env: Record<string, string>): EnvEntry[] {
  return Object.entries(env).map(([key, value]) => ({
    id: nextEnvId++,
    key,
    value,
    visible: false,
  }));
}

function entriesToEnvMap(entries: EnvEntry[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) map[key] = entry.value;
  }
  return map;
}

let nextArgId = 0;
interface ArgEntry {
  id: number;
  value: string;
}

function argsToEntries(args: string[]): ArgEntry[] {
  return args.map((value) => ({ id: nextArgId++, value }));
}

function entriesToArgs(entries: ArgEntry[]): string[] {
  return entries.flatMap((e) => e.value.trim().split(/\s+/)).filter(Boolean);
}

const personalDefaultsKey = (wsId: string) =>
  ["workspaces", wsId, "personal-agent-defaults"] as const;

// ─── Detail tab definitions ────────────────────────────────────────────────

type DefaultsTab = "instructions" | "env" | "custom_args" | "skills";

const defaultsTabs: { id: DefaultsTab; label: string; icon: typeof FileText }[] = [
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "env", label: "Environment", icon: KeyRound },
  { id: "custom_args", label: "Custom Args", icon: Terminal },
  { id: "skills", label: "Skills", icon: BookOpenText },
];

// ─── Shared form component ─────────────────────────────────────────────────

function DefaultsForm({
  config,
  readOnly,
  saving,
  onSave,
}: {
  config: AgentDefaultsConfig;
  readOnly: boolean;
  saving: boolean;
  onSave: (cfg: AgentDefaultsConfig) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));

  const [instructions, setInstructions] = useState(config.instructions ?? "");
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>(
    envMapToEntries(config.custom_env ?? {}),
  );
  const [argEntries, setArgEntries] = useState<ArgEntry[]>(
    argsToEntries(config.custom_args ?? []),
  );
  const [selectedSkills, setSelectedSkills] = useState<Set<string>>(
    new Set(config.skills ?? []),
  );
  const [activeTab, setActiveTab] = useState<DefaultsTab>("instructions");

  // Sync when config changes externally
  useEffect(() => {
    setInstructions(config.instructions ?? "");
    setEnvEntries(envMapToEntries(config.custom_env ?? {}));
    setArgEntries(argsToEntries(config.custom_args ?? []));
    setSelectedSkills(new Set(config.skills ?? []));
  }, [config]);

  const buildConfig = useCallback((): AgentDefaultsConfig => {
    const cfg: AgentDefaultsConfig = {};
    const instr = instructions.trim();
    if (instr) cfg.instructions = instr;
    const env = entriesToEnvMap(envEntries);
    if (Object.keys(env).length > 0) cfg.custom_env = env;
    const args = entriesToArgs(argEntries);
    if (args.length > 0) cfg.custom_args = args;
    if (selectedSkills.size > 0) cfg.skills = Array.from(selectedSkills);
    return cfg;
  }, [instructions, envEntries, argEntries, selectedSkills]);

  const isDirty = useCallback(() => {
    const normalize = (c: AgentDefaultsConfig) => {
      const n: AgentDefaultsConfig = {};
      if (c.instructions) n.instructions = c.instructions;
      if (c.custom_env && Object.keys(c.custom_env).length > 0) n.custom_env = c.custom_env;
      if (c.custom_args && c.custom_args.length > 0) n.custom_args = c.custom_args;
      if (c.skills && c.skills.length > 0) n.skills = c.skills;
      return n;
    };
    return JSON.stringify(normalize(buildConfig())) !== JSON.stringify(normalize(config));
  }, [buildConfig, config]);

  const handleSave = () => {
    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    if (new Set(keys).size < keys.length) {
      toast.error("Duplicate environment variable keys");
      return;
    }
    onSave(buildConfig());
  };

  // Env helpers
  const addEnvEntry = () => setEnvEntries([...envEntries, { id: nextEnvId++, key: "", value: "", visible: true }]);
  const removeEnvEntry = (i: number) => setEnvEntries(envEntries.filter((_, idx) => idx !== i));
  const updateEnvEntry = (i: number, field: "key" | "value", val: string) =>
    setEnvEntries(envEntries.map((e, idx) => (idx === i ? { ...e, [field]: val } : e)));
  const toggleEnvVisibility = (i: number) =>
    setEnvEntries(envEntries.map((e, idx) => (idx === i ? { ...e, visible: !e.visible } : e)));

  // Arg helpers
  const addArgEntry = () => setArgEntries([...argEntries, { id: nextArgId++, value: "" }]);
  const removeArgEntry = (i: number) => setArgEntries(argEntries.filter((_, idx) => idx !== i));
  const updateArgEntry = (i: number, value: string) =>
    setArgEntries(argEntries.map((e, idx) => (idx === i ? { ...e, value } : e)));

  // Skill toggle
  const toggleSkill = (skillId: string) => {
    setSelectedSkills((prev) => {
      const next = new Set(prev);
      if (next.has(skillId)) next.delete(skillId);
      else next.add(skillId);
      return next;
    });
  };

  return (
    <>
      {/* Tab bar */}
      <div className="flex border-b px-6">
        {defaultsTabs.map((tab) => (
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

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto p-6">
        {activeTab === "instructions" && (
          <div className="space-y-4">
            <div>
              <Label className="text-sm font-medium">Instructions</Label>
              <p className="text-xs text-muted-foreground mt-0.5">
                Default instructions applied to all agents.
              </p>
            </div>
            <textarea
              value={instructions}
              onChange={(e) => { if (!readOnly) setInstructions(e.target.value); }}
              readOnly={readOnly}
              placeholder={readOnly ? "No default instructions set" : "Add default instructions for all agents..."}
              className={`w-full min-h-[300px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y ${readOnly ? "cursor-default bg-muted/50" : ""}`}
            />
          </div>
        )}

        {activeTab === "env" && (
          <div className="max-w-lg space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <Label className="text-sm font-medium">Environment Variables</Label>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Default environment variables merged into every agent&apos;s environment.
                </p>
              </div>
              {!readOnly && (
                <Button variant="outline" size="sm" onClick={addEnvEntry} className="h-7 gap-1 text-xs">
                  <Plus className="h-3 w-3" />
                  Add
                </Button>
              )}
            </div>
            {envEntries.length > 0 ? (
              <div className="space-y-2">
                {envEntries.map((entry, index) => (
                  <div key={entry.id} className="flex items-center gap-2">
                    <Input
                      value={entry.key}
                      onChange={(e) => updateEnvEntry(index, "key", e.target.value)}
                      readOnly={readOnly}
                      placeholder="KEY"
                      className={`w-[40%] font-mono text-xs ${readOnly ? "bg-muted/50" : ""}`}
                    />
                    <div className="relative flex-1">
                      <Input
                        type={entry.visible ? "text" : "password"}
                        value={readOnly ? "****" : entry.value}
                        onChange={(e) => updateEnvEntry(index, "value", e.target.value)}
                        readOnly={readOnly}
                        placeholder="value"
                        className={`pr-8 font-mono text-xs ${readOnly ? "bg-muted/50" : ""}`}
                      />
                      {!readOnly && (
                        <button
                          type="button"
                          onClick={() => toggleEnvVisibility(index)}
                          className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        >
                          {entry.visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        </button>
                      )}
                    </div>
                    {!readOnly && (
                      <button
                        type="button"
                        onClick={() => removeEnvEntry(index)}
                        className="shrink-0 text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground italic">No environment variables configured.</p>
            )}
          </div>
        )}

        {activeTab === "custom_args" && (
          <div className="max-w-lg space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <Label className="text-sm font-medium">Custom Arguments</Label>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Default CLI arguments appended to every agent&apos;s launch command.
                </p>
              </div>
              {!readOnly && (
                <Button variant="outline" size="sm" onClick={addArgEntry} className="h-7 gap-1 text-xs">
                  <Plus className="h-3 w-3" />
                  Add
                </Button>
              )}
            </div>
            {argEntries.length > 0 ? (
              <div className="space-y-2">
                {argEntries.map((entry, index) => (
                  <div key={entry.id} className="flex items-center gap-2">
                    <Input
                      value={entry.value}
                      onChange={(e) => { if (!readOnly) updateArgEntry(index, e.target.value); }}
                      readOnly={readOnly}
                      placeholder="--flag value"
                      className={`flex-1 font-mono text-xs ${readOnly ? "bg-muted/50" : ""}`}
                    />
                    {!readOnly && (
                      <button
                        type="button"
                        onClick={() => removeArgEntry(index)}
                        className="shrink-0 text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground italic">No custom arguments configured.</p>
            )}
          </div>
        )}

        {activeTab === "skills" && (
          <div className="space-y-4">
            <div>
              <Label className="text-sm font-medium">Skills</Label>
              <p className="text-xs text-muted-foreground mt-0.5">
                Default skills assigned to all agents.
              </p>
            </div>
            {workspaceSkills.length > 0 ? (
              <div className="space-y-2">
                {workspaceSkills.map((skill) => (
                  <label
                    key={skill.id}
                    className={`flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors ${
                      !readOnly ? "cursor-pointer hover:bg-accent/50" : "cursor-default"
                    }`}
                  >
                    <Checkbox
                      checked={selectedSkills.has(skill.id)}
                      onCheckedChange={() => { if (!readOnly) toggleSkill(skill.id); }}
                      disabled={readOnly}
                    />
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
                      <FileText className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium">{skill.name}</div>
                      {skill.description && (
                        <div className="text-xs text-muted-foreground truncate">{skill.description}</div>
                      )}
                    </div>
                  </label>
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground italic">No workspace skills available.</p>
            )}
          </div>
        )}

        {/* Save button */}
        {!readOnly && (
          <div className="mt-6">
            <Button onClick={handleSave} disabled={!isDirty() || saving} size="sm">
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
              ) : (
                <Save className="h-3.5 w-3.5 mr-1.5" />
              )}
              Save
            </Button>
          </div>
        )}
      </div>
    </>
  );
}

// ─── Personal Defaults Detail ───────────────────────────────────────────────

export function PersonalDefaultsDetail() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const { data: personalDefaults, isLoading } = useQuery({
    queryKey: personalDefaultsKey(wsId),
    queryFn: () => api.getPersonalAgentDefaults(wsId),
  });

  const config = (personalDefaults?.config ?? {}) as AgentDefaultsConfig;

  const handleSave = async (cfg: AgentDefaultsConfig) => {
    setSaving(true);
    try {
      await api.updatePersonalAgentDefaults(wsId, cfg as Record<string, unknown>);
      qc.invalidateQueries({ queryKey: personalDefaultsKey(wsId) });
      toast.success("Personal agent defaults saved");
    } catch (e) {
      const msg = e instanceof ApiError ? e.message
        : e instanceof Error ? e.message
        : "Failed to save personal agent defaults";
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-blue-500/10">
          <Sliders className="h-4 w-4 text-blue-500" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Personal Defaults</h2>
        </div>
      </div>
      <DefaultsForm config={config} readOnly={false} saving={saving} onSave={handleSave} />
    </div>
  );
}

// ─── System Defaults Detail ─────────────────────────────────────────────────

export function SystemDefaultsDetail() {
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const agentDefaults = (workspace?.settings?.agent_defaults ?? {}) as AgentDefaultsConfig;

  const handleSave = async (cfg: AgentDefaultsConfig) => {
    if (!workspace) return;
    setSaving(true);
    try {
      const newSettings = { ...workspace.settings, agent_defaults: cfg };
      await api.updateWorkspace(workspace.id, { settings: newSettings });
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      toast.success("System agent defaults saved");
    } catch (e) {
      const msg = e instanceof ApiError ? e.message
        : e instanceof Error ? e.message
        : "Failed to save system agent defaults";
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <div className="flex h-7 w-7 items-center justify-center rounded-md bg-amber-500/10">
          <Settings2 className="h-4 w-4 text-amber-500" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">System Defaults</h2>
        </div>
      </div>
      <DefaultsForm config={agentDefaults} readOnly={false} saving={saving} onSave={handleSave} />
    </div>
  );
}

// ─── Other User's Defaults Detail (read-only + duplicate) ───────────────────

export function OtherDefaultsDetail({
  defaults,
  onDuplicate,
}: {
  defaults: AgentDefaultsWithUser;
  onDuplicate: () => void;
}) {
  const config = (defaults.config ?? {}) as AgentDefaultsConfig;

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <ActorAvatar actorType="member" actorId={defaults.user_id} size={28} className="rounded-md" />
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold truncate">{defaults.user_name}&apos;s Defaults</h2>
        </div>
        <Button variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={onDuplicate}>
          <Copy className="h-3 w-3" />
          Duplicate to Mine
        </Button>
      </div>
      <DefaultsForm config={config} readOnly={true} saving={false} onSave={() => {}} />
    </div>
  );
}

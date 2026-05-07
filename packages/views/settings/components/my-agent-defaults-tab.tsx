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
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";

interface AgentDefaultsConfig {
  instructions?: string;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  skills?: string[];
}

const personalDefaultsKey = (wsId: string) =>
  ["workspaces", wsId, "personal-agent-defaults"] as const;

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

export function MyAgentDefaultsTab() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));

  const { data: personalDefaults, isLoading } = useQuery({
    queryKey: personalDefaultsKey(wsId),
    queryFn: () => api.getPersonalAgentDefaults(wsId),
  });

  const config = (personalDefaults?.config ?? {}) as AgentDefaultsConfig;

  const [instructions, setInstructions] = useState("");
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>([]);
  const [argEntries, setArgEntries] = useState<ArgEntry[]>([]);
  const [selectedSkills, setSelectedSkills] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);

  // Sync state when API data loads
  useEffect(() => {
    if (!personalDefaults) return;
    const cfg = (personalDefaults.config ?? {}) as AgentDefaultsConfig;
    setInstructions(cfg.instructions ?? "");
    setEnvEntries(envMapToEntries(cfg.custom_env ?? {}));
    setArgEntries(argsToEntries(cfg.custom_args ?? []));
    setSelectedSkills(new Set(cfg.skills ?? []));
    setLoaded(true);
  }, [personalDefaults]);

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
    if (!loaded) return false;
    return JSON.stringify(buildConfig()) !== JSON.stringify(
      (() => {
        const cfg: AgentDefaultsConfig = {};
        if (config.instructions) cfg.instructions = config.instructions;
        if (config.custom_env && Object.keys(config.custom_env).length > 0) cfg.custom_env = config.custom_env;
        if (config.custom_args && config.custom_args.length > 0) cfg.custom_args = config.custom_args;
        if (config.skills && config.skills.length > 0) cfg.skills = config.skills;
        return cfg;
      })(),
    );
  }, [buildConfig, config, loaded]);

  const handleSave = async () => {
    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    if (new Set(keys).size < keys.length) {
      toast.error("Duplicate environment variable keys");
      return;
    }

    setSaving(true);
    try {
      await api.updatePersonalAgentDefaults(wsId, buildConfig() as Record<string, unknown>);
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

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-lg font-semibold">My Agent Defaults</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Your personal default configurations for agents in this workspace.
          These are merged on top of system defaults, with agent-level settings
          taking final precedence.
        </p>
      </div>

      {/* Instructions */}
      <section className="space-y-3">
        <div>
          <Label className="text-sm font-medium">Instructions</Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            Personal instructions appended after system defaults for every agent.
          </p>
        </div>
        <textarea
          value={instructions}
          onChange={(e) => setInstructions(e.target.value)}
          placeholder="Add your personal default instructions..."
          className="w-full min-h-[120px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y"
        />
      </section>

      {/* Environment Variables */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-sm font-medium">Environment Variables</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Personal environment variables merged into every agent&apos;s environment.
              Overrides system defaults for the same keys.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={addEnvEntry} className="h-7 gap-1 text-xs">
            <Plus className="h-3 w-3" />
            Add
          </Button>
        </div>
        {envEntries.length > 0 ? (
          <div className="space-y-2">
            {envEntries.map((entry, index) => (
              <div key={entry.id} className="flex items-center gap-2">
                <Input
                  value={entry.key}
                  onChange={(e) => updateEnvEntry(index, "key", e.target.value)}
                  placeholder="KEY"
                  className="w-[40%] font-mono text-xs"
                />
                <div className="relative flex-1">
                  <Input
                    type={entry.visible ? "text" : "password"}
                    value={entry.value}
                    onChange={(e) => updateEnvEntry(index, "value", e.target.value)}
                    placeholder="value"
                    className="pr-8 font-mono text-xs"
                  />
                  <button
                    type="button"
                    onClick={() => toggleEnvVisibility(index)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    {entry.visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                </div>
                <button
                  type="button"
                  onClick={() => removeEnvEntry(index)}
                  className="shrink-0 text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No personal environment variables configured.</p>
        )}
      </section>

      {/* Custom Args */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-sm font-medium">Custom Arguments</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Personal CLI arguments appended after system defaults.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={addArgEntry} className="h-7 gap-1 text-xs">
            <Plus className="h-3 w-3" />
            Add
          </Button>
        </div>
        {argEntries.length > 0 ? (
          <div className="space-y-2">
            {argEntries.map((entry, index) => (
              <div key={entry.id} className="flex items-center gap-2">
                <Input
                  value={entry.value}
                  onChange={(e) => updateArgEntry(index, e.target.value)}
                  placeholder="--flag value"
                  className="flex-1 font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={() => removeArgEntry(index)}
                  className="shrink-0 text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No personal custom arguments configured.</p>
        )}
      </section>

      {/* Skills */}
      <section className="space-y-3">
        <div>
          <Label className="text-sm font-medium">Skills</Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            Personal default skills added to every agent alongside system defaults.
          </p>
        </div>
        {workspaceSkills.length > 0 ? (
          <div className="space-y-2">
            {workspaceSkills.map((skill) => (
              <label
                key={skill.id}
                className="flex items-center gap-3 rounded-lg border px-4 py-3 cursor-pointer transition-colors hover:bg-accent/50"
              >
                <Checkbox
                  checked={selectedSkills.has(skill.id)}
                  onCheckedChange={() => toggleSkill(skill.id)}
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
          <p className="text-xs text-muted-foreground italic">
            No workspace skills available.
          </p>
        )}
      </section>

      {/* Save */}
      <Button onClick={handleSave} disabled={!isDirty() || saving} size="sm">
        {saving ? (
          <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
        ) : (
          <Save className="h-3.5 w-3.5 mr-1.5" />
        )}
        Save
      </Button>
    </div>
  );
}

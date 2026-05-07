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
  Info,
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
import { useAuthStore } from "@multica/core/auth";
import {
  memberListOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";

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

export function AgentDefaultsTab() {
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canEdit =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const agentDefaults = (workspace?.settings?.agent_defaults ?? {}) as AgentDefaultsConfig;

  const [instructions, setInstructions] = useState(agentDefaults.instructions ?? "");
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>(
    envMapToEntries(agentDefaults.custom_env ?? {}),
  );
  const [argEntries, setArgEntries] = useState<ArgEntry[]>(
    argsToEntries(agentDefaults.custom_args ?? []),
  );
  const [selectedSkills, setSelectedSkills] = useState<Set<string>>(
    new Set(agentDefaults.skills ?? []),
  );
  const [saving, setSaving] = useState(false);

  // Sync when workspace changes
  useEffect(() => {
    const defaults = (workspace?.settings?.agent_defaults ?? {}) as AgentDefaultsConfig;
    setInstructions(defaults.instructions ?? "");
    setEnvEntries(envMapToEntries(defaults.custom_env ?? {}));
    setArgEntries(argsToEntries(defaults.custom_args ?? []));
    setSelectedSkills(new Set(defaults.skills ?? []));
  }, [workspace?.id, workspace?.updated_at]);

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
    return JSON.stringify(buildConfig()) !== JSON.stringify(
      (() => {
        const d = agentDefaults;
        const cfg: AgentDefaultsConfig = {};
        if (d.instructions) cfg.instructions = d.instructions;
        if (d.custom_env && Object.keys(d.custom_env).length > 0) cfg.custom_env = d.custom_env;
        if (d.custom_args && d.custom_args.length > 0) cfg.custom_args = d.custom_args;
        if (d.skills && d.skills.length > 0) cfg.skills = d.skills;
        return cfg;
      })(),
    );
  }, [buildConfig, agentDefaults]);

  const handleSave = async () => {
    if (!workspace) return;

    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    if (new Set(keys).size < keys.length) {
      toast.error("Duplicate environment variable keys");
      return;
    }

    setSaving(true);
    try {
      const newSettings = { ...workspace.settings, agent_defaults: buildConfig() };
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
    <div className="space-y-8">
      <div>
        <h2 className="text-lg font-semibold">System Agent Defaults</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Default configurations applied to all agents in this workspace.
          Individual agent settings take precedence over these defaults.
        </p>
      </div>

      {!canEdit && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2.5">
          <Info className="h-3.5 w-3.5 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
          <p className="text-xs text-amber-950 dark:text-amber-100">
            Only workspace owners and admins can edit system agent defaults.
          </p>
        </div>
      )}

      {/* Instructions */}
      <section className="space-y-3">
        <div>
          <Label className="text-sm font-medium">Instructions</Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            Default instructions prepended to every agent&apos;s instructions.
          </p>
        </div>
        <textarea
          value={instructions}
          onChange={(e) => { if (canEdit) setInstructions(e.target.value); }}
          readOnly={!canEdit}
          placeholder={canEdit ? "Add default instructions for all agents..." : "No default instructions set"}
          className={`w-full min-h-[120px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y ${!canEdit ? "cursor-default bg-muted/50" : ""}`}
        />
      </section>

      {/* Environment Variables */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-sm font-medium">Environment Variables</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Default environment variables merged into every agent&apos;s environment.
            </p>
          </div>
          {canEdit && (
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
                  readOnly={!canEdit}
                  placeholder="KEY"
                  className={`w-[40%] font-mono text-xs ${!canEdit ? "bg-muted/50" : ""}`}
                />
                <div className="relative flex-1">
                  <Input
                    type={entry.visible ? "text" : "password"}
                    value={canEdit ? entry.value : "****"}
                    onChange={(e) => updateEnvEntry(index, "value", e.target.value)}
                    readOnly={!canEdit}
                    placeholder="value"
                    className={`pr-8 font-mono text-xs ${!canEdit ? "bg-muted/50" : ""}`}
                  />
                  <button
                    type="button"
                    onClick={() => canEdit && toggleEnvVisibility(index)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    {entry.visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                  </button>
                </div>
                {canEdit && (
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
          <p className="text-xs text-muted-foreground italic">No default environment variables configured.</p>
        )}
      </section>

      {/* Custom Args */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-sm font-medium">Custom Arguments</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              Default CLI arguments appended to every agent&apos;s launch command.
            </p>
          </div>
          {canEdit && (
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
                  onChange={(e) => { if (canEdit) updateArgEntry(index, e.target.value); }}
                  readOnly={!canEdit}
                  placeholder="--flag value"
                  className={`flex-1 font-mono text-xs ${!canEdit ? "bg-muted/50" : ""}`}
                />
                {canEdit && (
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
          <p className="text-xs text-muted-foreground italic">No default custom arguments configured.</p>
        )}
      </section>

      {/* Skills */}
      <section className="space-y-3">
        <div>
          <Label className="text-sm font-medium">Skills</Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            Default skills automatically assigned to all agents. Agents may also
            have additional skills configured individually.
          </p>
        </div>
        {workspaceSkills.length > 0 ? (
          <div className="space-y-2">
            {workspaceSkills.map((skill) => (
              <label
                key={skill.id}
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors ${
                  canEdit ? "cursor-pointer hover:bg-accent/50" : "cursor-default"
                }`}
              >
                <Checkbox
                  checked={selectedSkills.has(skill.id)}
                  onCheckedChange={() => { if (canEdit) toggleSkill(skill.id); }}
                  disabled={!canEdit}
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
            No workspace skills available. Create skills in the workspace to assign them as defaults.
          </p>
        )}
      </section>

      {/* Save */}
      {canEdit && (
        <Button onClick={handleSave} disabled={!isDirty() || saving} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5 mr-1.5" />
          )}
          Save
        </Button>
      )}
    </div>
  );
}

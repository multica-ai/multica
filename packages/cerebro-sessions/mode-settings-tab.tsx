"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { skillListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import type { SessionMode } from "./modes";
import {
  type ModeConfig,
  modeWorkflowOptions,
  modeConfigsOptions,
  usePublishMode,
  useRestoreMode,
  useSaveModeDraft,
} from "./mode-config";

const MODES: { value: SessionMode; label: string; description: string }[] = [
  { value: "plan", label: "Plan", description: "Investigate and prepare an executable plan." },
  { value: "build", label: "Build", description: "Implement and verify the requested outcome." },
  { value: "research", label: "Research", description: "Read-only investigation with cited evidence." },
  { value: "review", label: "Review", description: "Independent review without changing the work." },
];

type Choice = { id: string; name: string };
interface EditorProps {
  config: ModeConfig;
  onChange: (config: ModeConfig) => void;
  onSave: () => void;
  onPublish: () => void;
  canManage: boolean;
  busy?: boolean;
  workflows?: Choice[];
  skills?: Choice[];
}

function toggle(values: string[], value: string): string[] {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
}

export function ModeConfigEditor({
  config, onChange, onSave, onPublish, canManage, busy = false, workflows = [], skills = [],
}: EditorProps) {
  const patch = <K extends keyof ModeConfig>(key: K, value: ModeConfig[K]) =>
    onChange({ ...config, [key]: value });
  const disabled = !canManage || busy;

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Label htmlFor="mode-instructions">Instructions</Label>
        <Textarea id="mode-instructions" rows={8} value={config.instruction} disabled={disabled}
          onChange={(event) => patch("instruction", event.target.value)} />
        <p className="text-xs text-muted-foreground">Injected into every agent run using this Mode.</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="mode-model">Model</Label>
          <Input id="mode-model" value={config.model} placeholder="Use the agent default" disabled={disabled}
            onChange={(event) => patch("model", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>Thinking level</Label>
          <Select value={config.thinking_level} disabled={disabled}
            onValueChange={(value) => patch("thinking_level", value as ModeConfig["thinking_level"])}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>{["low", "medium", "high", "xhigh"].map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}</SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="mode-timeout">Timeout (minutes)</Label>
          <Input id="mode-timeout" type="number" min={1} max={1440} value={config.timeout_minutes} disabled={disabled}
            onChange={(event) => patch("timeout_minutes", Number(event.target.value))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="mode-turns">Maximum turns</Label>
          <Input id="mode-turns" type="number" min={1} max={200} value={config.max_turns} disabled={disabled}
            onChange={(event) => patch("max_turns", Number(event.target.value))} />
        </div>
      </div>

      <div className="flex items-center justify-between rounded-lg border p-3">
        <div><Label htmlFor="mode-write">Allow writes</Label><p className="text-xs text-muted-foreground">Permit code, data and external mutations subject to permissions.</p></div>
        <Switch id="mode-write" checked={config.allows_write} disabled={disabled} onCheckedChange={(value) => patch("allows_write", value)} />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="mode-tools">Allowed tools</Label>
          <Textarea id="mode-tools" rows={4} value={config.allowed_tools.join("\n")} disabled={disabled}
            placeholder="One tool key per line" onChange={(event) => patch("allowed_tools", event.target.value.split("\n").map((v) => v.trim()).filter(Boolean))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="mode-data">Data sources</Label>
          <Textarea id="mode-data" rows={4} value={config.data_sources.join("\n")} disabled={disabled}
            placeholder="One approved source per line" onChange={(event) => patch("data_sources", event.target.value.split("\n").map((v) => v.trim()).filter(Boolean))} />
        </div>
      </div>

      <div className="space-y-2">
        <Label>Approval policy</Label>
        <Select value={config.approval_policy} disabled={disabled}
          onValueChange={(value) => patch("approval_policy", value as ModeConfig["approval_policy"])}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="inherit">Inherit workspace permissions</SelectItem>
            <SelectItem value="require">Require approval for mutations</SelectItem>
            <SelectItem value="deny_external">Deny external mutations</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2">
        <Label htmlFor="mode-workflow">Workflow</Label>
        <select id="mode-workflow" className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={config.workflow_id} disabled={disabled}
          onChange={(event) => patch("workflow_id", event.target.value)}>
          <option value="">No workflow</option>
          {workflows.map((workflow) => <option key={workflow.id} value={workflow.id}>{workflow.name}</option>)}
        </select>
        <p className="text-xs text-muted-foreground">The existing Workflow recipe used when this Mode starts work.</p>
      </div>

      <div className="space-y-2">
        <Label>Required evaluations</Label>
        <div className="max-h-48 space-y-1 overflow-y-auto rounded-lg border p-2">
          {skills.length === 0 ? <p className="p-2 text-xs text-muted-foreground">No workspace skills available.</p> : skills.map((skill) => (
            <label key={skill.id} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted/50">
              <input type="checkbox" checked={config.eval_skill_ids.includes(skill.id)} disabled={disabled}
                onChange={() => patch("eval_skill_ids", toggle(config.eval_skill_ids, skill.id))} />
              {skill.name}
            </label>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap justify-end gap-2 border-t pt-4">
        <Button variant="outline" onClick={onSave} disabled={disabled}>Save draft</Button>
        <Button onClick={onPublish} disabled={disabled || !config.instruction.trim()}>Publish version</Button>
      </div>
    </div>
  );
}

export function ModeSettingsTab() {
  const workspace = useCurrentWorkspace();
  const workspaceId = workspace?.id ?? "";
  const { role } = useCurrentMember(workspaceId);
  const canManage = role === "owner" || role === "admin";
  const modesQuery = useQuery(modeConfigsOptions(workspaceId));
  const workflowsQuery = useQuery(modeWorkflowOptions(workspaceId));
  const skillsQuery = useQuery(skillListOptions(workspaceId));
  const save = useSaveModeDraft(workspaceId);
  const publish = usePublishMode(workspaceId);
  const restore = useRestoreMode(workspaceId);
  const [selected, setSelected] = useState<SessionMode>("plan");
  const record = modesQuery.data?.modes.find((item) => item.mode === selected);
  const [draft, setDraft] = useState<ModeConfig | null>(null);
  const [description, setDescription] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    if (record) setDraft(record.draft ?? record.active);
  }, [record]);

  const workflows = (workflowsQuery.data?.workflows ?? [])
    .filter((workflow) => workflow.workflow_type === "issue_loop" && workflow.enabled)
    .map((workflow) => ({ id: workflow.id, name: workflow.name }));
  const skills = (skillsQuery.data ?? []).map((skill) => ({ id: skill.id, name: skill.name }));
  const busy = save.isPending || publish.isPending || restore.isPending;

  const saveDraft = async () => {
    if (!draft) return;
    setMessage("");
    try { await save.mutateAsync({ mode: selected, config: draft }); setMessage("Draft saved."); }
    catch { setMessage("Draft could not be saved."); }
  };
  const publishVersion = async () => {
    if (!draft) return;
    setMessage("");
    try {
      await save.mutateAsync({ mode: selected, config: draft });
      await publish.mutateAsync({ mode: selected, description });
      setDescription("");
      setMessage("Version published. New runs will use it.");
    } catch { setMessage("Version could not be published."); }
  };

  return (
    <div className="space-y-5">
      <div><h2 className="text-lg font-medium">Modes</h2><p className="text-sm text-muted-foreground">Version the instructions, runtime limits, Workflow and required evaluations used by Issue and Chat sessions.</p></div>
      <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
        <div className="space-y-2">{MODES.map((mode) => (
          <button key={mode.value} type="button" onClick={() => setSelected(mode.value)}
            className={`w-full rounded-lg border p-3 text-left transition-colors ${selected === mode.value ? "border-primary bg-primary/5" : "hover:bg-muted/50"}`}>
            <div className="flex items-center justify-between"><span className="font-medium">{mode.label}</span>{modesQuery.data?.modes.find((item) => item.mode === mode.value)?.draft && <Badge variant="secondary">Draft</Badge>}</div>
            <p className="mt-1 text-xs text-muted-foreground">{mode.description}</p>
          </button>
        ))}</div>
        <Card><CardContent className="space-y-5 pt-6">
          {record && draft ? <>
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div><p className="font-medium">Published version {record.active_version}</p><p className="text-xs text-muted-foreground">A run keeps the version it started with.</p></div>
              <div className="space-y-1"><Label htmlFor="mode-version-note">Version note</Label><Input id="mode-version-note" value={description} disabled={!canManage || busy} placeholder="What changed?" onChange={(event) => setDescription(event.target.value)} /></div>
            </div>
            <ModeConfigEditor config={draft} onChange={setDraft} onSave={saveDraft} onPublish={publishVersion} canManage={canManage} busy={busy} workflows={workflows} skills={skills} />
            {message && <p className="text-sm" role="status">{message}</p>}
            <div className="space-y-2 border-t pt-5"><h3 className="font-medium">Version history</h3>
              {record.versions.map((version) => <div key={version.id || version.version} className="flex items-center justify-between gap-3 rounded-lg border p-3"><div><p className="text-sm font-medium">Version {version.version}</p><p className="text-xs text-muted-foreground">{version.description || "No version note"}</p></div><Button size="sm" variant="outline" disabled={!canManage || busy || version.version === record.active_version} onClick={() => restore.mutate({ mode: selected, version: version.version })}>Restore</Button></div>)}
            </div>
          </> : <p className="text-sm text-muted-foreground">{modesQuery.isLoading ? "Loading Modes…" : "Mode configuration is unavailable."}</p>}
          {!canManage && <p className="text-xs text-muted-foreground">Only workspace owners and admins can publish Mode configuration.</p>}
        </CardContent></Card>
      </div>
    </div>
  );
}

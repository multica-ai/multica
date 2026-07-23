"use client";

import { useEffect, useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  Check,
  ChevronDown,
  RotateCcw,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { runtimeListOptions, runtimeModelsOptions } from "@multica/core/runtimes";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { skillListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { toolPolicyTableOptions } from "@multica/cerebro-tool-policy/core";
import { useDataSourceScopeConfig, useScopeOptions } from "@multica/cerebro-tool-policy/data-source-scope";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import type { SessionMode } from "./modes";
import {
  type ModeConfig,
  type ThinkingLevel,
  modeWorkflowOptions,
  modeConfigsOptions,
  usePublishMode,
  useRestoreMode,
  useSaveModeDraft,
} from "./mode-config";

export const MODES: { value: SessionMode; label: string; description: string }[] = [
  { value: "plan", label: "Plan", description: "Turn a request into a clear, step-by-step plan." },
  { value: "build", label: "Build", description: "Make the change and check that it works." },
  { value: "research", label: "Research", description: "Gather evidence without changing files or data." },
  { value: "review", label: "Review", description: "Check a proposed change for risks and quality." },
];

export const THINKING_LEVELS: { value: ThinkingLevel; name: string; description: string }[] = [
  { value: "low", name: "Light", description: "Fast answers for simple tasks." },
  { value: "medium", name: "Balanced", description: "A good default for most work." },
  { value: "high", name: "Deep", description: "More careful reasoning for complex work." },
  { value: "xhigh", name: "Most thorough", description: "The most detailed reasoning; takes longer." },
];

export type Choice = { id: string; name: string; description?: string };
export type ModeValidation = Partial<
  Record<"instruction" | "timeout_minutes" | "max_turns", string>
>;

export function validateModeConfig(config: ModeConfig): ModeValidation {
  const errors: ModeValidation = {};
  if (!config.instruction.trim()) errors.instruction = "Add instructions before saving.";
  if (!Number.isInteger(config.timeout_minutes) || config.timeout_minutes < 0) {
    errors.timeout_minutes = "Use 0 or a positive whole number.";
  }
  if (!Number.isInteger(config.max_turns) || config.max_turns < 0) {
    errors.max_turns = "Use 0 or a positive whole number.";
  }
  return errors;
}

function numberInputValue(value: number): number | "" {
  return Number.isNaN(value) ? "" : value;
}

function parseNumberInput(value: string): number {
  return value === "" ? Number.NaN : Number(value);
}

function toggle(values: string[], value: string): string[] {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
}

function configsEqual(left: ModeConfig, right: ModeConfig): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function readableDate(value: string): string {
  if (!value) return "Date unavailable";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Date unavailable";
  return new Intl.DateTimeFormat("en", { dateStyle: "medium" }).format(date);
}

function selectedNames(values: string[], choices: Choice[]): Choice[] {
  const byId = new Map(choices.map((choice) => [choice.id, choice]));
  return values.map((value) => byId.get(value) ?? { id: value, name: value });
}

function SearchableMultiChoice({
  label,
  values,
  choices,
  emptyLabel,
  helper,
  onChange,
  disabled,
}: {
  label: string;
  values: string[];
  choices: Choice[];
  emptyLabel: string;
  helper: string;
  onChange: (values: string[]) => void;
  disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const selected = selectedNames(values, choices);
  const filtered = choices.filter((choice) => {
    const needle = search.trim().toLowerCase();
    return !needle || `${choice.name} ${choice.id} ${choice.description ?? ""}`.toLowerCase().includes(needle);
  });

  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div className="flex min-h-9 flex-wrap items-center gap-1 rounded-lg border border-input bg-transparent p-1.5">
        {selected.length === 0 ? (
          <span className="px-1 text-sm text-muted-foreground">{emptyLabel}</span>
        ) : selected.map((choice) => (
          <Badge key={choice.id} variant="secondary" className="max-w-full gap-1">
            <span className="truncate">{choice.name}</span>
            {!disabled && (
              <button
                type="button"
                className="rounded-full p-0.5 hover:bg-background/50"
                aria-label={`Remove ${choice.name}`}
                onClick={() => onChange(values.filter((value) => value !== choice.id))}
              >
                <X className="size-3" />
              </button>
            )}
          </Badge>
        ))}
        <Popover open={open} onOpenChange={(next) => { setOpen(next); if (!next) setSearch(""); }}>
          <PopoverTrigger
            type="button"
            aria-label={`Choose ${label}`}
            disabled={disabled}
            className="ml-auto inline-flex size-7 items-center justify-center rounded-md hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
          >
            <ChevronDown className="size-4 text-muted-foreground" />
          </PopoverTrigger>
          <PopoverContent align="end" className="w-80 p-2">
            <ChoicePopover
              label={label}
              search={search}
              onSearch={setSearch}
              choices={filtered}
              selected={values}
              onToggle={(value) => onChange(toggle(values, value))}
              emptyMessage={choices.length === 0 ? "Catalog unavailable. Existing values are kept." : "No matches."}
            />
          </PopoverContent>
        </Popover>
      </div>
      <p className="text-xs text-muted-foreground">{helper}</p>
    </div>
  );
}

function ChoicePopover({
  label,
  search,
  onSearch,
  choices,
  selected,
  onToggle,
  emptyMessage,
}: {
  label: string;
  search: string;
  onSearch: (value: string) => void;
  choices: Choice[];
  selected: string[];
  onToggle: (value: string) => void;
  emptyMessage: string;
}) {
  // This component is rendered beside the trigger so it can be used for both
  // single and multi-choice fields without exposing raw IDs in the form.
  return (
    <div role="dialog" aria-label={`${label} options`} className="rounded-lg border bg-popover p-2 shadow-md">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label={`${label} search`}
          value={search}
          onChange={(event) => onSearch(event.target.value)}
          placeholder={`Search ${label.toLowerCase()}`}
          className="h-8 pl-8"
        />
      </div>
      <div className="mt-1 max-h-56 overflow-y-auto">
        {choices.length === 0 ? <p className="p-3 text-xs text-muted-foreground">{emptyMessage}</p> : choices.map((choice) => {
          const isSelected = selected.includes(choice.id);
          return (
            <button
              key={choice.id}
              type="button"
              aria-pressed={isSelected}
              className="flex w-full items-start gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-muted"
              onClick={() => onToggle(choice.id)}
            >
              <span className={`mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border ${isSelected ? "border-primary bg-primary text-primary-foreground" : "border-input"}`}>
                {isSelected && <Check className="size-3" />}
              </span>
              <span className="min-w-0">
                <span className="block truncate font-medium">{choice.name}</span>
                {choice.description && <span className="block text-xs text-muted-foreground">{choice.description}</span>}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function SearchableSingleChoice({
  label,
  value,
  choices,
  onChange,
  disabled,
}: {
  label: string;
  value: string;
  choices: Choice[];
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const selected = choices.find((choice) => choice.id === value);
  const filtered = choices.filter((choice) => `${choice.name} ${choice.id}`.toLowerCase().includes(search.trim().toLowerCase()));
  const customValue = search.trim() && !choices.some((choice) => choice.id === search.trim());

  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          type="button"
          disabled={disabled}
          className="flex h-9 w-full items-center justify-between rounded-lg border border-input bg-transparent px-2.5 text-left text-sm hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          <span className="truncate">{selected?.name ?? (value ? `${value} (custom)` : "Agent default")}</span>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-[var(--anchor-width)] p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              autoFocus
              aria-label={`${label} search`}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search models"
              className="h-8 pl-8"
            />
          </div>
          <div className="mt-1 max-h-56 overflow-y-auto">
            <button type="button" className="flex w-full rounded-md px-2 py-2 text-left text-sm hover:bg-muted" onClick={() => { onChange(""); setOpen(false); setSearch(""); }}>
              <span className="font-medium">Agent default</span>
              {!value && <Check className="ml-auto size-4" />}
            </button>
            {filtered.map((choice) => (
              <button key={choice.id} type="button" className="flex w-full rounded-md px-2 py-2 text-left text-sm hover:bg-muted" onClick={() => { onChange(choice.id); setOpen(false); setSearch(""); }}>
                <span><span className="block font-medium">{choice.name}</span>{choice.description && <span className="block text-xs text-muted-foreground">{choice.description}</span>}</span>
                {value === choice.id && <Check className="ml-auto size-4" />}
              </button>
            ))}
            {customValue && (
              <button type="button" className="flex w-full rounded-md px-2 py-2 text-left text-sm hover:bg-muted" onClick={() => { onChange(search.trim()); setOpen(false); setSearch(""); }}>
                Use “{search.trim()}” as a custom model
              </button>
            )}
            {filtered.length === 0 && !customValue && <p className="p-3 text-xs text-muted-foreground">No models found.</p>}
          </div>
        </PopoverContent>
      </Popover>
      <p className="text-xs text-muted-foreground">Choose a workspace model or keep the Agent default.</p>
    </div>
  );
}

interface EditorProps {
  config: ModeConfig;
  onChange: (config: ModeConfig) => void;
  onSave: () => void | Promise<void>;
  onPublish: () => void | Promise<void>;
  canManage: boolean;
  busy?: boolean;
  workflows?: Choice[];
  skills?: Choice[];
  modelChoices?: Choice[];
  toolChoices?: Choice[];
  dataSourceChoices?: Choice[];
  dirty?: boolean;
}

export function ModeConfigEditor({
  config,
  onChange,
  onSave,
  onPublish,
  canManage,
  busy = false,
  workflows = [],
  skills = [],
  modelChoices = [],
  toolChoices = [],
  dataSourceChoices = [],
  dirty = false,
}: EditorProps) {
  const patch = <K extends keyof ModeConfig>(key: K, value: ModeConfig[K]) => onChange({ ...config, [key]: value });
  const disabled = !canManage || busy;
  const validation = validateModeConfig(config);
  const hasErrors = Object.keys(validation).length > 0;
  const thinking = THINKING_LEVELS.find((level) => level.value === config.thinking_level);
  const [evaluationSearch, setEvaluationSearch] = useState("");
  const filteredSkills = skills
    .filter((skill) => `${skill.name} ${skill.id}`.toLowerCase().includes(evaluationSearch.trim().toLowerCase()))
    .sort((left, right) => Number(config.eval_skill_ids.includes(right.id)) - Number(config.eval_skill_ids.includes(left.id)) || left.name.localeCompare(right.name));

  return (
    <div className="space-y-8">
      {!canManage && (
        <div role="note" className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/40 p-3 text-sm text-muted-foreground">
          <ShieldCheck className="mt-0.5 size-4 shrink-0" />
          <span>This page is view-only. Workspace owners and admins can change Modes.</span>
        </div>
      )}

      <section aria-labelledby="mode-core-behavior" className="space-y-5">
        <div>
          <h3 id="mode-core-behavior" className="font-medium">Core behavior</h3>
          <p className="text-xs text-muted-foreground">Set how the agent should work and the limits for this Mode.</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="mode-instructions">Instructions</Label>
          <Textarea id="mode-instructions" rows={8} value={config.instruction} disabled={disabled} aria-invalid={Boolean(validation.instruction)} onChange={(event) => patch("instruction", event.target.value)} />
          <p className="text-xs text-muted-foreground">Tell the agent what good work looks like in this Mode.</p>
          {validation.instruction && <p className="text-xs text-destructive" role="alert">{validation.instruction}</p>}
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <div className="space-y-2">
            {modelChoices.length > 0 ? <SearchableSingleChoice label="Model" value={config.model} choices={modelChoices} disabled={disabled} onChange={(value) => patch("model", value)} /> : <>
              <Label htmlFor="mode-model">Model</Label>
              <Input id="mode-model" value={config.model} placeholder="Use the Agent default" disabled={disabled} onChange={(event) => patch("model", event.target.value)} />
              <p className="text-xs text-muted-foreground">The model catalog is unavailable; your custom value is kept.</p>
            </>}
          </div>
          <div className="space-y-2">
            <Label>Thinking level</Label>
            <Select value={config.thinking_level} disabled={disabled} onValueChange={(value) => patch("thinking_level", value as ThinkingLevel)}>
              <SelectTrigger className="w-full"><SelectValue>{thinking?.name ?? "Choose a level"}</SelectValue></SelectTrigger>
              <SelectContent>{THINKING_LEVELS.map((level) => <SelectItem key={level.value} value={level.value}><span><span className="block font-medium">{level.name}</span><span className="block text-xs text-muted-foreground">{level.description}</span></span></SelectItem>)}</SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="mode-timeout">Timeout (minutes)</Label>
            <Input id="mode-timeout" type="number" min={0} value={numberInputValue(config.timeout_minutes)} disabled={disabled} aria-invalid={Boolean(validation.timeout_minutes)} onChange={(event) => patch("timeout_minutes", parseNumberInput(event.target.value))} />
            <p className="text-xs text-muted-foreground">Set 0 for no time limit.</p>
            {validation.timeout_minutes && <p className="text-xs text-destructive" role="alert">{validation.timeout_minutes}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="mode-turns">Maximum turns</Label>
            <Input id="mode-turns" type="number" min={0} value={numberInputValue(config.max_turns)} disabled={disabled} aria-invalid={Boolean(validation.max_turns)} onChange={(event) => patch("max_turns", parseNumberInput(event.target.value))} />
            <p className="text-xs text-muted-foreground">A turn is one agent response. Set 0 for no turn limit.</p>
            {validation.max_turns && <p className="text-xs text-destructive" role="alert">{validation.max_turns}</p>}
          </div>
        </div>
      </section>

      <section aria-labelledby="mode-access-and-approvals" className="space-y-5 border-t pt-6">
        <div>
          <h3 id="mode-access-and-approvals" className="font-medium">Access and approvals</h3>
          <p className="text-xs text-muted-foreground">Control which actions and connected sources this Mode can use.</p>
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3">
          <div><Label htmlFor="mode-write">Allow writes</Label><p className="text-xs text-muted-foreground">Allow changes to code or connected data when permissions permit.</p></div>
          <Switch id="mode-write" checked={config.allows_write} disabled={disabled} onCheckedChange={(value) => patch("allows_write", value)} />
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <SearchableMultiChoice label="Allowed tools" values={config.allowed_tools} choices={toolChoices} emptyLabel="All tools allowed" helper="Leave empty to allow every tool permitted by workspace policy." onChange={(values) => patch("allowed_tools", values)} disabled={disabled} />
          <SearchableMultiChoice label="Data sources" values={config.data_sources} choices={dataSourceChoices} emptyLabel="All data sources allowed" helper="Leave empty to allow every connected source permitted by workspace policy." onChange={(values) => patch("data_sources", values)} disabled={disabled} />
        </div>

        <div className="space-y-2">
          <Label>Approval policy</Label>
          <Select value={config.approval_policy} disabled={disabled} onValueChange={(value) => patch("approval_policy", value as ModeConfig["approval_policy"]) }>
            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="inherit">Inherit workspace permissions</SelectItem>
              <SelectItem value="require">Ask before changes</SelectItem>
              <SelectItem value="deny_external">Block external changes</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">This adds a safety check; it does not grant permissions.</p>
        </div>
      </section>

      <section aria-labelledby="mode-workflow-and-quality" className="space-y-5 border-t pt-6">
        <div>
          <h3 id="mode-workflow-and-quality" className="font-medium">Workflow and quality</h3>
          <p className="text-xs text-muted-foreground">Choose the workflow and checks that apply before the Mode is used.</p>
        </div>
        <div className="space-y-2">
          <Label>Workflow</Label>
          <Select value={config.workflow_id || "__none__"} disabled={disabled} onValueChange={(value) => { const next = value ?? "__none__"; patch("workflow_id", next === "__none__" ? "" : next); }}>
            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">No workflow</SelectItem>
              {workflows.map((workflow) => <SelectItem key={workflow.id} value={workflow.id}>{workflow.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">Choose the shared Workflow recipe to use when this Mode starts work.</p>
        </div>

        <div className="space-y-2">
          <div className="flex items-end justify-between gap-2"><div><Label>Required evaluations</Label><p className="text-xs text-muted-foreground">Choose checks that must pass before this Mode is used.</p></div><span className="text-xs text-muted-foreground">{config.eval_skill_ids.length} selected</span></div>
          <div className="relative"><Search className="pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2 text-muted-foreground" /><Input aria-label="Search required evaluations" value={evaluationSearch} onChange={(event) => setEvaluationSearch(event.target.value)} placeholder="Search evaluations" className="h-8 pl-8" disabled={disabled} /></div>
          <div className="max-h-56 space-y-1 overflow-y-auto rounded-lg border p-2">
            {skills.length === 0 ? <p className="p-2 text-xs text-muted-foreground">No workspace evaluations available.</p> : filteredSkills.length === 0 ? <p className="p-2 text-xs text-muted-foreground">No evaluations match your search.</p> : filteredSkills.map((skill) => (
              <label key={skill.id} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted/50">
                <input type="checkbox" checked={config.eval_skill_ids.includes(skill.id)} disabled={disabled} onChange={() => patch("eval_skill_ids", toggle(config.eval_skill_ids, skill.id))} />
                <span className="min-w-0 truncate">{skill.name}</span>
              </label>
            ))}
          </div>
        </div>
      </section>

      <div className="sticky bottom-0 z-10 -mx-4 border-t bg-card/95 px-4 py-3 backdrop-blur-sm">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className={`flex items-center gap-1.5 text-xs ${dirty ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground"}`} role="status">
            {dirty ? <><AlertCircle className="size-3.5" /> Unsaved changes</> : <><Check className="size-3.5" /> All changes saved</>}
          </span>
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" onClick={onSave} disabled={disabled || hasErrors}>Save draft</Button>
            <Button onClick={onPublish} disabled={disabled || hasErrors}>Publish version</Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function ModeDialogs({
  pendingMode,
  onKeepEditing,
  onDiscard,
  onSaveDraft,
  publishOpen,
  publishNote,
  onPublishNote,
  onCancelPublish,
  onConfirmPublish,
  publishBusy,
  restoreVersion,
  onCancelRestore,
  onConfirmRestore,
  restoreBusy,
}: {
  pendingMode: SessionMode | null;
  onKeepEditing: () => void;
  onDiscard: () => void;
  onSaveDraft: () => void | Promise<void>;
  publishOpen: boolean;
  publishNote: string;
  onPublishNote: (value: string) => void;
  onCancelPublish: () => void;
  onConfirmPublish: () => void | Promise<void>;
  publishBusy: boolean;
  restoreVersion: string | null;
  onCancelRestore: () => void;
  onConfirmRestore: () => void | Promise<void>;
  restoreBusy: boolean;
}) {
  return <>
    <AlertDialog open={Boolean(pendingMode)} onOpenChange={(open) => { if (!open) onKeepEditing(); }}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>You have unsaved changes</AlertDialogTitle><AlertDialogDescription>Choose what to do before switching Mode.</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onKeepEditing}>Keep editing</AlertDialogCancel>
          <Button variant="outline" onClick={onSaveDraft}>Save draft</Button>
          <AlertDialogAction variant="destructive" onClick={onDiscard}>Discard changes</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <AlertDialog open={publishOpen} onOpenChange={(open) => { if (!open) onCancelPublish(); }}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader><AlertDialogTitle>Publish this Mode?</AlertDialogTitle><AlertDialogDescription>New Issue and Chat sessions will use this version. Existing runs keep the version they started with.</AlertDialogDescription></AlertDialogHeader>
        <div className="space-y-2"><Label htmlFor="publish-version-note">Version note</Label><Input id="publish-version-note" value={publishNote} onChange={(event) => onPublishNote(event.target.value)} placeholder="What changed?" /><p className="text-xs text-muted-foreground">A short note helps people understand this change later.</p></div>
        <AlertDialogFooter><AlertDialogCancel onClick={onCancelPublish}>Cancel</AlertDialogCancel><AlertDialogAction onClick={onConfirmPublish} disabled={publishBusy}>{publishBusy ? "Publishing…" : "Publish version"}</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>

    <AlertDialog open={Boolean(restoreVersion)} onOpenChange={(open) => { if (!open) onCancelRestore(); }}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>Restore version {restoreVersion}?</AlertDialogTitle><AlertDialogDescription>New runs will use it. The current published version stays in the history.</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel onClick={onCancelRestore}>Cancel</AlertDialogCancel><AlertDialogAction onClick={onConfirmRestore} disabled={restoreBusy}>{restoreBusy ? "Restoring…" : "Restore version"}</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </>;
}

export function ModeSettingsTab() {
  const workspace = useCurrentWorkspace();
  const workspaceId = workspace?.id ?? "";
  const { role } = useCurrentMember(workspaceId);
  const canManage = role === "owner" || role === "admin";
  const modesQuery = useQuery(modeConfigsOptions(workspaceId));
  const workflowsQuery = useQuery(modeWorkflowOptions(workspaceId));
  const skillsQuery = useQuery(skillListOptions(workspaceId));
  const membersQuery = useQuery(memberListOptions(workspaceId));
  const runtimesQuery = useQuery(runtimeListOptions(workspaceId));
  const toolPolicyQuery = useQuery(toolPolicyTableOptions(workspaceId, {}));
  const dataScopeConfig = useDataSourceScopeConfig(workspaceId);
  const dataScopeQuery = useScopeOptions(workspaceId, dataScopeConfig, true);
  const save = useSaveModeDraft(workspaceId);
  const publish = usePublishMode(workspaceId);
  const restore = useRestoreMode(workspaceId);
  const [selected, setSelected] = useState<SessionMode>("plan");
  const [draftMode, setDraftMode] = useState<SessionMode | null>(null);
  const [draft, setDraft] = useState<ModeConfig | null>(null);
  const [publishNote, setPublishNote] = useState("");
  const [publishOpen, setPublishOpen] = useState(false);
  const [pendingMode, setPendingMode] = useState<SessionMode | null>(null);
  const [restoreVersion, setRestoreVersion] = useState<string | null>(null);

  const record = modesQuery.data?.modes.find((item) => item.mode === selected);
  const currentDraft = draftMode === selected ? draft : null;

  useEffect(() => {
    if (record && draftMode !== selected) {
      setDraft(record.draft ?? record.active);
      setDraftMode(selected);
      setPublishNote("");
    }
  }, [draftMode, record, selected]);

  const workflows = (workflowsQuery.data?.workflows ?? [])
    .filter((workflow) => workflow.workflow_type === "issue_loop" && workflow.enabled)
    .map((workflow) => ({ id: workflow.id, name: workflow.name }));
  const skills = (skillsQuery.data ?? []).map((skill) => ({ id: skill.id, name: skill.name, description: skill.description }));
  const modelQueries = useModelQueries(runtimesQuery.data ?? []);
  const modelChoices = useMemo(() => {
    const byId = new Map<string, Choice>();
    for (const query of modelQueries) {
      for (const model of query.data?.models ?? []) byId.set(model.id, { id: model.id, name: model.label, description: model.provider });
    }
    return [...byId.values()].sort((left, right) => left.name.localeCompare(right.name));
  }, [modelQueries]);
  const toolChoices = useMemo(() => {
    const byId = new Map<string, Choice>();
    for (const row of toolPolicyQuery.data ?? []) byId.set(row.tool_key, { id: row.tool_key, name: row.title || row.tool_key, description: row.description });
    return [...byId.values()].sort((left, right) => left.name.localeCompare(right.name));
  }, [toolPolicyQuery.data]);
  const dataSourceChoices = useMemo(() => dataScopeQuery.options.map((option) => ({ id: option.id, name: option.name, description: option.folder })), [dataScopeQuery.options]);
  const memberNames = useMemo(() => {
    const names = new Map<string, string>();
    for (const member of membersQuery.data ?? []) {
      names.set(member.id, member.name);
      names.set(member.user_id, member.name);
    }
    return names;
  }, [membersQuery.data]);
  const busy = save.isPending || publish.isPending || restore.isPending;
  const dirty = Boolean(record && currentDraft && !configsEqual(currentDraft, record.draft ?? record.active));

  const switchMode = (next: SessionMode) => {
    setSelected(next);
    setDraftMode(null);
    setDraft(null);
    setPendingMode(null);
  };

  const saveDraft = async (showToast = true): Promise<boolean> => {
    if (!currentDraft || Object.keys(validateModeConfig(currentDraft)).length > 0) {
      if (showToast) toast.error("Fix the highlighted fields before saving.");
      return false;
    }
    try {
      const saved = await save.mutateAsync({ mode: selected, config: currentDraft });
      setDraft(saved.draft ?? currentDraft);
      if (showToast) toast.success("Draft saved.");
      return true;
    } catch {
      if (showToast) toast.error("Draft could not be saved. Try again.");
      return false;
    }
  };

  const publishVersion = async () => {
    if (!currentDraft || Object.keys(validateModeConfig(currentDraft)).length > 0) return;
    try {
      const saved = await save.mutateAsync({ mode: selected, config: currentDraft });
      setDraft(saved.draft ?? currentDraft);
      const published = await publish.mutateAsync({ mode: selected, description: publishNote.trim() });
      setDraft(published.draft ?? published.active ?? currentDraft);
      setPublishOpen(false);
      setPublishNote("");
      toast.success("Version published. New runs will use it.");
    } catch {
      toast.error("Version could not be published. Try again.");
    }
  };

  const restoreSelectedVersion = async () => {
    if (!restoreVersion) return;
    try {
      const restored = await restore.mutateAsync({ mode: selected, version: restoreVersion });
      setDraft(restored.draft ?? restored.active);
      setDraftMode(selected);
      toast.success(`Version ${restoreVersion} restored.`);
      setRestoreVersion(null);
    } catch {
      toast.error("Version could not be restored. Try again.");
    }
  };

  const requestModeSwitch = (next: SessionMode) => {
    if (next === selected) return;
    if (dirty) setPendingMode(next);
    else switchMode(next);
  };

  if (modesQuery.isError) {
    return <div className="space-y-3 rounded-xl border border-dashed p-6"><h2 className="font-medium">Modes are unavailable</h2><p className="text-sm text-muted-foreground">We could not load the Mode settings.</p><Button variant="outline" onClick={() => modesQuery.refetch()}><RotateCcw className="size-4" /> Try again</Button></div>;
  }

  return (
    <div className="space-y-5">
      <div><h2 className="text-lg font-medium">Modes</h2><p className="text-sm text-muted-foreground">Choose how Issue and Chat sessions work: instructions, safety checks, tools, and required evaluations.</p></div>
      {!canManage && <div role="note" className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/40 p-3 text-sm text-muted-foreground"><ShieldCheck className="mt-0.5 size-4 shrink-0" /><span>You are viewing these settings. Only workspace owners and admins can save or publish changes.</span></div>}
      <div className="xl:hidden sticky top-2 z-20 rounded-lg border bg-card/95 p-2 shadow-sm backdrop-blur-sm">
        <Label className="sr-only">Active Mode</Label>
        <Select value={selected} onValueChange={(value) => requestModeSwitch(value as SessionMode)}><SelectTrigger aria-label="Active Mode" className="w-full"><SelectValue /></SelectTrigger><SelectContent>{MODES.map((mode) => <SelectItem key={mode.value} value={mode.value}>{mode.label}</SelectItem>)}</SelectContent></Select>
        <p className="px-1 pt-2 text-xs text-muted-foreground">{MODES.find((mode) => mode.value === selected)?.description}</p>
      </div>
      <div className="grid gap-4 xl:grid-cols-[220px_minmax(0,1fr)]">
        <div className="hidden space-y-2 xl:block">{MODES.map((mode) => {
          const modeRecord = modesQuery.data?.modes.find((item) => item.mode === mode.value);
          return <button key={mode.value} type="button" aria-current={selected === mode.value ? "page" : undefined} onClick={() => requestModeSwitch(mode.value)} className={`w-full rounded-lg border p-3 text-left transition-colors ${selected === mode.value ? "border-primary bg-primary/5" : "hover:bg-muted/50"}`}>
            <div className="flex items-center justify-between gap-2"><span className="font-medium">{mode.label}</span><span className="flex gap-1">{selected === mode.value && <Badge>Active</Badge>}{modeRecord?.draft && <Badge variant="secondary">Draft</Badge>}</span></div>
            <p className="mt-1 text-xs text-muted-foreground">{mode.description}</p>
          </button>;
        })}</div>
        <Card><CardContent className="space-y-5 pt-6">
          {record && currentDraft ? <>
            <div className="flex flex-wrap items-start justify-between gap-3 border-b pb-4"><div><p className="font-medium">{MODES.find((mode) => mode.value === selected)?.label} settings</p><p className="text-xs text-muted-foreground">Published version {record.active_version}. New sessions use the published version.</p></div><span className="text-xs text-muted-foreground">{dirty ? "Draft needs saving" : "Ready to use"}</span></div>
            <ModeConfigEditor config={currentDraft} onChange={setDraft} onSave={() => { void saveDraft(); }} onPublish={() => { setPublishOpen(true); }} canManage={canManage} busy={busy} workflows={workflows} skills={skills} modelChoices={modelChoices} toolChoices={toolChoices} dataSourceChoices={dataSourceChoices} dirty={dirty} />
            <div className="space-y-2 border-t pt-5"><div className="flex items-center justify-between gap-2"><div><h3 className="font-medium">Version history</h3><p className="text-xs text-muted-foreground">Restore an earlier version if the current setup is not right.</p></div><Badge variant="outline">{record.versions.length} versions</Badge></div>
              {record.versions.length === 0 ? <p className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">No published versions yet.</p> : record.versions.map((version) => <div key={version.id || version.version} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><p className="text-sm font-medium">Version {version.version}</p>{version.version === record.active_version && <Badge>Active</Badge>}</div><p className="text-xs text-muted-foreground">{version.description || "No version note"}</p><p className="mt-1 text-xs text-muted-foreground">{readableDate(version.created_at)} · {memberNames.get(version.created_by) ?? "Workspace admin"}</p></div><Button size="sm" variant="outline" disabled={!canManage || busy || version.version === record.active_version} onClick={() => setRestoreVersion(version.version)}><RotateCcw className="size-3.5" /> Restore</Button></div>)}
            </div>
          </> : <p className="text-sm text-muted-foreground">{modesQuery.isLoading ? "Loading Modes…" : "Mode configuration is unavailable."}</p>}
        </CardContent></Card>
      </div>
      <ModeDialogs pendingMode={pendingMode} onKeepEditing={() => setPendingMode(null)} onDiscard={() => { if (pendingMode) switchMode(pendingMode); }} onSaveDraft={async () => { const saved = await saveDraft(); if (saved) switchMode(pendingMode!); }} publishOpen={publishOpen} publishNote={publishNote} onPublishNote={setPublishNote} onCancelPublish={() => { setPublishOpen(false); setPublishNote(""); }} onConfirmPublish={publishVersion} publishBusy={busy} restoreVersion={restoreVersion} onCancelRestore={() => setRestoreVersion(null)} onConfirmRestore={restoreSelectedVersion} restoreBusy={busy} />
    </div>
  );
}

function useModelQueries(runtimes: { id: string; status: string }[]) {
  return useQueries({ queries: runtimes.filter((runtime) => runtime.status === "online").map((runtime) => runtimeModelsOptions(runtime.id)) });
}

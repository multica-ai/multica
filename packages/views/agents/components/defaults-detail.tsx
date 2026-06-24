"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import {
  BookOpenText,
  Copy,
  Eye,
  EyeOff,
  FileText,
  History,
  KeyRound,
  Loader2,
  Lock,
  Plus,
  Save,
  Settings2,
  Sliders,
  Terminal,
  Trash2,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import type { AgentDefaultsWithUser, AgentConfigTemplate } from "@multica/core/types";
import { ContentEditor } from "../../editor/content-editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import {
  InstructionsHistoryDialog,
  instructionsHistoryKey,
} from "./instructions-history-dialog";
import { configTemplateKeys } from "./config-template-keys";
import { SkillPickerList } from "./skill-picker-list";

// ─── Types & helpers ────────────────────────────────────────────────────────

interface AgentDefaultsConfig {
  instructions?: string;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  skills?: string[];
}

// Stable empty defaults. The tab components reset their local state via
// `useEffect([prop])` when the prop reference changes; without these, the
// `?? {}` / `?? []` fallbacks mint a fresh object on every render and the
// effect fires on each keystroke — wiping the row the user just typed into
// (env/custom_args) or the skill they just toggled (skills). Module-level
// constants keep the identity stable across renders, so the reset only fires
// when the template actually changes value (switch / refetch after save).
const EMPTY_ENV: Record<string, string> = {};
const EMPTY_ARGS: string[] = [];
const EMPTY_SKILLS: string[] = [];

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

// ─── Tab definitions ────────────────────────────────────────────────────────

type DefaultsTab = "instructions" | "env" | "custom_args" | "skills";

const TAB_LABEL_KEY: Record<DefaultsTab, "instructions" | "environment" | "custom_args" | "skills"> = {
  instructions: "instructions",
  env: "environment",
  custom_args: "custom_args",
  skills: "skills",
};

const defaultsTabs: {
  id: DefaultsTab;
  icon: typeof FileText;
}[] = [
  { id: "instructions", icon: FileText },
  { id: "env", icon: KeyRound },
  { id: "custom_args", icon: Terminal },
  { id: "skills", icon: BookOpenText },
];

// ─── Tab content wrapper (matches agent-overview-pane TabContent) ───────────

function TabContent({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto flex h-full max-w-2xl flex-col p-4 md:p-6">{children}</div>
  );
}

// ─── Instructions tab ──────────────────────────────────────────────────────

function DefaultsInstructionsTab({
  value: initialValue,
  readOnly,
  saving,
  showHistory,
  onOpenHistory,
  onSave,
  onDirtyChange,
}: {
  value: string;
  readOnly: boolean;
  saving: boolean;
  showHistory: boolean;
  onOpenHistory: () => void;
  onSave: (instructions: string) => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [value, setValue] = useState(initialValue);
  const isDirty = value !== initialValue;

  useEffect(() => {
    setValue(initialValue);
  }, [initialValue]);

  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  return (
    <div className="flex h-full flex-col gap-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.instructions.intro)}
      </p>

      {readOnly ? (
        <div className="flex-1 min-h-0 overflow-y-auto rounded-md border bg-muted/50 px-4 py-3">
          <pre className="whitespace-pre-wrap font-mono text-sm text-muted-foreground">
            {value || t(($) => $.tab_body.instructions.placeholder)}
          </pre>
        </div>
      ) : (
        <>
          <div className="flex-1 min-h-0 overflow-y-auto rounded-md border bg-background px-4 py-3 transition-colors focus-within:border-input">
            <ContentEditor
              key={`defaults-instructions-${initialValue}`}
              defaultValue={initialValue}
              onUpdate={setValue}
              placeholder={t(($) => $.tab_body.instructions.placeholder)}
              debounceMs={150}
              disableMentions
              className="min-h-full"
            />
          </div>

          <div className="flex items-center justify-end gap-3">
            {isDirty && (
              <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
            )}
            {showHistory && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onOpenHistory}
                className="gap-1"
              >
                <History className="h-3.5 w-3.5" />
                {t(($) => $.history.action)}
              </Button>
            )}
            <Button
              size="sm"
              onClick={() => onSave(value)}
              disabled={!isDirty || saving}
            >
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Save className="h-3.5 w-3.5" />
              )}
              {t(($) => $.tab_body.common.save)}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

// ─── Environment tab ────────────────────────────────────────────────────────

function DefaultsEnvTab({
  env: originalEnv,
  readOnly,
  saving,
  onSave,
  onDirtyChange,
}: {
  env: Record<string, string>;
  readOnly: boolean;
  saving: boolean;
  onSave: (env: Record<string, string>) => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>(
    envMapToEntries(originalEnv),
  );

  useEffect(() => {
    setEnvEntries(envMapToEntries(originalEnv));
  }, [originalEnv]);

  const currentEnvMap = entriesToEnvMap(envEntries);
  const dirty = JSON.stringify(currentEnvMap) !== JSON.stringify(originalEnv);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const addEnvEntry = () => {
    setEnvEntries([
      ...envEntries,
      { id: nextEnvId++, key: "", value: "", visible: true },
    ]);
  };

  const removeEnvEntry = (index: number) => {
    setEnvEntries(envEntries.filter((_, i) => i !== index));
  };

  const updateEnvEntry = (index: number, field: "key" | "value", val: string) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, [field]: val } : entry,
      ),
    );
  };

  const toggleEnvVisibility = (index: number) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, visible: !entry.visible } : entry,
      ),
    );
  };

  const handleSave = () => {
    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    if (new Set(keys).size < keys.length) {
      toast.error(t(($) => $.tab_body.env.duplicate_keys_toast));
      return;
    }
    onSave(currentEnvMap);
  };

  if (readOnly) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.env.intro_readonly)}
        </p>
        {envEntries.length > 0 ? (
          <div className="space-y-2">
            {envEntries.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2">
                <Input
                  value={entry.key}
                  readOnly
                  className="w-[40%] bg-muted font-mono text-xs"
                />
                <div className="relative flex-1">
                  <Input
                    type="password"
                    value="****"
                    readOnly
                    className="bg-muted pr-8 font-mono text-xs"
                  />
                  <Lock className="absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs italic text-muted-foreground">
            {t(($) => $.tab_body.env.empty_readonly)}
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.env.intro_prefix)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            {"ANTHROPIC_API_KEY"}
          </code>
          {t(($) => $.tab_body.env.intro_separator)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            {"ANTHROPIC_BASE_URL"}
          </code>
          {t(($) => $.tab_body.env.intro_suffix)}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEnvEntry}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          {t(($) => $.tab_body.common.add)}
        </Button>
      </div>

      {envEntries.length > 0 && (
        <div className="space-y-2">
          {envEntries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.key}
                onChange={(e) => updateEnvEntry(index, "key", e.target.value)}
                placeholder={t(($) => $.tab_body.env.key_placeholder)}
                className="w-[40%] font-mono text-xs"
              />
              <div className="relative flex-1">
                <Input
                  type={entry.visible ? "text" : "password"}
                  value={entry.value}
                  onChange={(e) => updateEnvEntry(index, "value", e.target.value)}
                  placeholder={t(($) => $.tab_body.env.value_placeholder)}
                  className="pr-8 font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={() => toggleEnvVisibility(index)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label={entry.visible ? t(($) => $.tab_body.env.hide_value_aria) : t(($) => $.tab_body.env.show_value_aria)}
                >
                  {entry.visible ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => removeEnvEntry(index)}
                className="text-muted-foreground hover:text-destructive"
                aria-label={t(($) => $.tab_body.env.remove_aria)}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

// ─── Custom Args tab ────────────────────────────────────────────────────────

function DefaultsCustomArgsTab({
  args: originalArgs,
  readOnly,
  saving,
  onSave,
  onDirtyChange,
}: {
  args: string[];
  readOnly: boolean;
  saving: boolean;
  onSave: (args: string[]) => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [entries, setEntries] = useState<ArgEntry[]>(
    argsToEntries(originalArgs),
  );

  useEffect(() => {
    setEntries(argsToEntries(originalArgs));
  }, [originalArgs]);

  const currentArgs = entriesToArgs(entries);
  const dirty = JSON.stringify(currentArgs) !== JSON.stringify(originalArgs);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const addEntry = () => {
    setEntries([...entries, { id: nextArgId++, value: "" }]);
  };

  const removeEntry = (index: number) => {
    setEntries(entries.filter((_, i) => i !== index));
  };

  const updateEntry = (index: number, value: string) => {
    setEntries(
      entries.map((entry, i) => (i === index ? { ...entry, value } : entry)),
    );
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.custom_args.intro)}
        </p>
        {!readOnly && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addEntry}
            className="shrink-0"
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.tab_body.common.add)}
          </Button>
        )}
      </div>

      {entries.length > 0 && (
        <div className="space-y-2">
          {entries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.value}
                onChange={(e) => { if (!readOnly) updateEntry(index, e.target.value); }}
                readOnly={readOnly}
                placeholder={t(($) => $.tab_body.custom_args.input_placeholder)}
                className={`flex-1 font-mono text-xs ${readOnly ? "bg-muted/50" : ""}`}
              />
              {!readOnly && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => removeEntry(index)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label={t(($) => $.tab_body.custom_args.remove_aria)}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      {entries.length === 0 && readOnly && (
        <p className="text-xs text-muted-foreground italic">
          {t(($) => $.tab_body.custom_args.intro)}
        </p>
      )}

      {!readOnly && (
        <div className="flex items-center justify-end gap-3">
          {dirty && (
            <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
          )}
          <Button onClick={() => onSave(currentArgs)} disabled={!dirty || saving} size="sm">
            {saving ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            {t(($) => $.tab_body.common.save)}
          </Button>
        </div>
      )}
    </div>
  );
}

// ─── Skills tab ─────────────────────────────────────────────────────────────

function DefaultsSkillsTab({
  selectedSkillIds,
  readOnly,
  saving,
  onSave,
  onDirtyChange,
}: {
  selectedSkillIds: string[];
  readOnly: boolean;
  saving: boolean;
  onSave: (skills: string[]) => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [], isLoading } = useQuery(skillListOptions(wsId));

  const [selected, setSelected] = useState<Set<string>>(new Set(selectedSkillIds));

  useEffect(() => {
    setSelected(new Set(selectedSkillIds));
  }, [selectedSkillIds]);

  const dirty = JSON.stringify([...selected].sort()) !== JSON.stringify([...selectedSkillIds].sort());

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const toggleSkill = (skillId: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(skillId)) next.delete(skillId);
      else next.add(skillId);
      return next;
    });
  };

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.skills.intro)}
      </p>

      {/* Reuses the same searchable SkillPickerList the agent detail page's
          SkillAddDialog / create-form use — one surface owns search + row
          rendering, so a workspace with many skills stays searchable here too. */}
      <SkillPickerList
        skills={workspaceSkills}
        selectedIds={selected}
        onToggle={(skill) => { if (!readOnly) toggleSkill(skill.id); }}
        loading={isLoading}
        disabled={readOnly}
        emptyMessage={t(($) => $.tab_body.skills.empty_title)}
      />

      {!readOnly && (
        <div className="flex items-center justify-end gap-3">
          {dirty && (
            <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
          )}
          <Button onClick={() => onSave(Array.from(selected))} disabled={!dirty || saving} size="sm">
            {saving ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            {t(($) => $.tab_body.common.save)}
          </Button>
        </div>
      )}
    </div>
  );
}

// ─── Main form shell (tab bar + unsaved-changes guard) ──────────────────────

export function DefaultsForm({
  config,
  readOnly,
  saving,
  historyScope,
  templateId,
  onRestoreInstructions,
  onSaveField,
}: {
  config: AgentDefaultsConfig;
  readOnly: boolean;
  saving: boolean;
  historyScope?: "personal" | "system";
  /** Template whose instructions history the History button opens. Required
   *  when historyScope is set — history is template-owned (migration 127). */
  templateId?: string;
  onRestoreInstructions?: (instructions: string) => Promise<void>;
  onSaveField: (field: keyof AgentDefaultsConfig, value: unknown) => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const [activeTab, setActiveTab] = useState<DefaultsTab>("instructions");
  const [activeDirty, setActiveDirty] = useState(false);
  const [pendingTab, setPendingTab] = useState<DefaultsTab | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);

  const requestTabChange = (next: DefaultsTab) => {
    if (next === activeTab) return;
    if (activeDirty) {
      setPendingTab(next);
      return;
    }
    setActiveTab(next);
  };

  const commitTabChange = () => {
    if (pendingTab) {
      setActiveTab(pendingTab);
      setActiveDirty(false);
      setPendingTab(null);
    }
  };

  return (
    <>
      {/* Tab bar — matches agent-overview-pane */}
      <div className="flex shrink-0 items-center gap-0 overflow-x-auto border-b px-2 md:px-4">
        {defaultsTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => requestTabChange(tab.id)}
            className={`flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2.5 text-xs font-medium transition-colors ${
              activeTab === tab.id
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <tab.icon className="h-3.5 w-3.5" />
            {t(($) => $.tabs[TAB_LABEL_KEY[tab.id]])}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        {activeTab === "instructions" && (
          <TabContent>
            <DefaultsInstructionsTab
              value={config.instructions ?? ""}
              readOnly={readOnly}
              saving={saving}
              showHistory={!readOnly && historyScope !== undefined}
              onOpenHistory={() => setHistoryOpen(true)}
              onSave={(v) => onSaveField("instructions", v)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {activeTab === "env" && (
          <TabContent>
            <DefaultsEnvTab
              env={config.custom_env ?? EMPTY_ENV}
              readOnly={readOnly}
              saving={saving}
              onSave={(v) => onSaveField("custom_env", v)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {activeTab === "custom_args" && (
          <TabContent>
            <DefaultsCustomArgsTab
              args={config.custom_args ?? EMPTY_ARGS}
              readOnly={readOnly}
              saving={saving}
              onSave={(v) => onSaveField("custom_args", v)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {activeTab === "skills" && (
          <TabContent>
            <DefaultsSkillsTab
              selectedSkillIds={config.skills ?? EMPTY_SKILLS}
              readOnly={readOnly}
              saving={saving}
              onSave={(v) => onSaveField("skills", v)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
      </div>

      {/* Unsaved-changes guard */}
      {pendingTab !== null && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setPendingTab(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.tabs.discard_dialog_title)}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tabs.discard_dialog_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.tabs.discard_keep)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={commitTabChange}
              >
                {t(($) => $.tabs.discard_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

      {historyScope && templateId && onRestoreInstructions && (
        <InstructionsHistoryDialog
          workspaceId={wsId}
          templateId={templateId}
          scope={historyScope}
          open={historyOpen}
          currentContent={config.instructions ?? ""}
          onOpenChange={setHistoryOpen}
          onRestore={onRestoreInstructions}
        />
      )}
    </>
  );
}

// ─── Template config editor ─────────────────────────────────────────────────
// Edits a single config template through the same structured DefaultsForm the
// legacy defaults used — no raw JSON. The default template of a scope IS that
// scope's default config (per migration 125 + resolveAgentConfig), so editing
// the default template also records an instructions version. The History
// affordance therefore shows only for the default template (history is
// scope-based and tracks the default config, not plain templates).

export function TemplateConfigEditor({
  template,
  scope,
  readOnly,
}: {
  template: AgentConfigTemplate;
  scope: "system" | "personal";
  readOnly: boolean;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const config = useMemo(
    () => (template.config ?? {}) as AgentDefaultsConfig,
    [template.config],
  );

  const getSavedToast = useCallback((field: keyof AgentDefaultsConfig) => {
    switch (field) {
      case "instructions":
        return t(($) => $.tab_body.instructions.saved_toast);
      case "custom_args":
        return t(($) => $.tab_body.custom_args.saved_toast);
      case "skills":
        return t(($) => $.tab_body.skills.saved_toast);
      case "custom_env":
      default:
        return t(($) => $.tab_body.env.saved_toast);
    }
  }, [t]);

  const getSaveFailedToast = useCallback((field: keyof AgentDefaultsConfig) => {
    switch (field) {
      case "instructions":
        return t(($) => $.tab_body.instructions.save_failed_toast);
      case "custom_args":
        return t(($) => $.tab_body.custom_args.save_failed_toast);
      case "skills":
        return t(($) => $.tab_body.skills.save_failed_toast);
      case "custom_env":
      default:
        return t(($) => $.tab_body.env.save_failed_toast);
    }
  }, [t]);

  const saveConfig = useCallback(async (
    newConfig: AgentDefaultsConfig,
    field: keyof AgentDefaultsConfig,
    options?: { showSavedToast?: boolean },
  ) => {
    setSaving(true);
    try {
      await api.updateAgentConfigTemplate(template.id, {
        config: newConfig as Record<string, unknown>,
      });
      const invalidations: Promise<unknown>[] = [
        qc.invalidateQueries({ queryKey: configTemplateKeys.all(wsId) }),
      ];
      if (template.is_default) {
        invalidations.push(
          qc.invalidateQueries({ queryKey: instructionsHistoryKey(wsId, scope) }),
        );
      }
      await Promise.all(invalidations);
      if (options?.showSavedToast !== false) {
        toast.success(getSavedToast(field));
      }
    } catch (e) {
      const msg = e instanceof ApiError ? e.message
        : e instanceof Error ? e.message
        : getSaveFailedToast(field);
      toast.error(msg);
      throw e;
    } finally {
      setSaving(false);
    }
  }, [wsId, qc, template.id, template.is_default, scope, getSavedToast, getSaveFailedToast]);

  const handleSaveField = useCallback(async (field: keyof AgentDefaultsConfig, value: unknown) => {
    await saveConfig({ ...config, [field]: value }, field);
  }, [config, saveConfig]);

  const handleRestoreInstructions = useCallback(async (instructions: string) => {
    await saveConfig({ ...config, instructions }, "instructions", { showSavedToast: false });
  }, [config, saveConfig]);

  const saveDescription = useCallback(async (next: string) => {
    const trimmed = next.trim();
    if (trimmed === (template.description ?? "")) return;
    setSaving(true);
    try {
      await api.updateAgentConfigTemplate(template.id, { description: trimmed });
      await qc.invalidateQueries({ queryKey: configTemplateKeys.all(wsId) });
      toast.success(t(($) => $.template.description_saved_toast));
    } catch (e) {
      const msg = e instanceof ApiError ? e.message
        : e instanceof Error ? e.message
        : t(($) => $.template.update_failed);
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  }, [template.id, template.description, qc, wsId, t]);

  // Every template carries its own instructions history (migration 127), so
  // the History affordance shows for any editable template — not only the
  // default one. The old `template.is_default` gate was a leftover from when
  // history was scope/member-owned.
  const historyScope = !readOnly ? scope : undefined;

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex shrink-0 flex-col gap-1 border-b px-4 py-2.5">
        <div className="flex items-center gap-3">
          <div
            className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${
              scope === "system" ? "bg-amber-500/10" : "bg-blue-500/10"
            }`}
          >
            {scope === "system" ? (
              <Settings2 className="h-4 w-4 text-amber-500" />
            ) : (
              <Sliders className="h-4 w-4 text-blue-500" />
            )}
          </div>
          <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{template.name}</h2>
        </div>
        {/* Description — editable inline, saved on blur. Read-only viewers see
            plain text (or nothing when empty). Kept out of the tabbed form
            because it's template metadata, not part of the merged config. */}
        {readOnly ? (
          template.description ? (
            <p className="pl-10 text-xs text-muted-foreground">{template.description}</p>
          ) : null
        ) : (
          <Input
            key={`desc-${template.id}-${template.description ?? ""}`}
            defaultValue={template.description ?? ""}
            placeholder={t(($) => $.template.description_placeholder)}
            onBlur={(e) => void saveDescription(e.target.value)}
            className="ml-10 h-7 text-xs text-muted-foreground"
          />
        )}
      </div>
      <DefaultsForm
        config={config}
        readOnly={readOnly}
        saving={saving}
        historyScope={historyScope}
        templateId={template.id}
        onRestoreInstructions={historyScope ? handleRestoreInstructions : undefined}
        onSaveField={handleSaveField}
      />
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
  const { t } = useT("agents");
  const config = (defaults.config ?? {}) as AgentDefaultsConfig;

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <ActorAvatar actorType="member" actorId={defaults.user_id} size={28} className="rounded-md" />
        <div className="min-w-0 flex-1">
          {/* eslint-disable-next-line i18next/no-literal-string */}
          <h2 className="text-sm font-semibold truncate">{defaults.user_name}&apos;s Defaults</h2>
        </div>
        <Button variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={onDuplicate}>
          <Copy className="h-3 w-3" />
          {t(($) => $.defaults.duplicate_action)}
        </Button>
      </div>
      <DefaultsForm config={config} readOnly={true} saving={false} onSaveField={() => {}} />
    </div>
  );
}

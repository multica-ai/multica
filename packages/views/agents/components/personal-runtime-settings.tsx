"use client";

import { useId, useState } from "react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useQuery } from "@tanstack/react-query";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import {
  agentRuntimePreferenceOptions,
  runtimeModelsOptions,
  useUpdateAgentRuntimePreference,
} from "@multica/core/runtimes";
import type { Agent, AgentRuntime, AgentRuntimePreference, MemberWithUser } from "@multica/core/types";
import { SettingsCard, SettingsRow, SettingsSection } from "../../settings/components/settings-layout";
import { useT } from "../../i18n";
import { RuntimePicker } from "./inspector/runtime-picker";

export function PersonalRuntimeSettings({ agent, runtimes, members, currentUserId }: {
  agent: Agent;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
}) {
  const { t } = useT("agents");
  const [expandedFor, setExpandedFor] = useState<string | null>(null);
  const scope = JSON.stringify([agent.workspace_id, agent.id, currentUserId]);
  const isOwner = agent.owner_id === currentUserId;
  const member = members.find((candidate) => candidate.user_id === currentUserId);
  const canInvoke = !!member && canAssignAgentToIssue(agent, {
    userId: currentUserId, role: member.role,
  }).allowed;
  const preference = useQuery({
    ...agentRuntimePreferenceOptions(agent.workspace_id, agent.id),
    enabled: canInvoke,
  });
  const updatePreference = useUpdateAgentRuntimePreference(agent.workspace_id, agent.id);
  const choices = runtimes.filter((runtime) =>
    runtime.owner_id === currentUserId && runtime.workspace_id === agent.workspace_id,
  );
  if (!canInvoke) return null;

  const selectedId = preference.data?.runtimeId ?? null;
  const selectedRuntime = choices.find((runtime) => runtime.id === selectedId);
  const unavailable = selectedId !== null && !selectedRuntime;
  const failed = preference.isError || (!preference.isPending && !preference.data);

  const selectRuntime = (runtimeId: string | null) => updatePreference.mutate(runtimeId, {
    onSuccess: (saved) => {
      if (saved && saved.runtimeId === null) setExpandedFor(null);
    },
  });

  if (isOwner && !preference.isPending && !failed && selectedId === null && expandedFor !== scope) {
    return (
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-caption text-muted-foreground">{t(($) => $.personal_runtime.settings.owner_default)}</p>
        <Button variant="ghost" size="sm" onClick={() => setExpandedFor(scope)}>
          {t(($) => $.personal_runtime.settings.owner_customize)}
        </Button>
      </div>
    );
  }

  return (
    <SettingsSection title={t(($) => $.personal_runtime.settings.title)} description={t(($) => $.personal_runtime.settings.hint)}>
      {isOwner && selectedId !== null && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-surface-border px-4 py-3">
          <p className="text-body">{t(($) => $.personal_runtime.settings.owner_override)}</p>
          <Button variant="outline" size="sm" disabled={updatePreference.isPending} onClick={() => selectRuntime(null)}>
            {t(($) => $.personal_runtime.settings.owner_restore)}
          </Button>
        </div>
      )}
      <SettingsCard>
        <SettingsRow label={t(($) => $.personal_runtime.settings.label)} size="select-wide">
          {preference.isPending ? (
            <p className="text-body text-muted-foreground">{t(($) => $.create_dialog.runtime_loading)}</p>
          ) : failed ? (
            <p role="alert" className="text-body text-destructive">{t(($) => $.personal_runtime.settings.load_failed)}</p>
          ) : (
            <div className="space-y-2">
              <RuntimePicker
                variant="field"
                showLabel={false}
                value={selectedId ?? ""}
                defaultLabel={t(($) => $.personal_runtime.settings.use_default)}
                runtimes={choices}
                members={members}
                currentUserId={currentUserId}
                canEdit={!updatePreference.isPending}
                onChange={(id) => selectRuntime(id || null)}
              />
              {unavailable && <p role="alert" className="text-caption text-destructive">{t(($) => $.personal_runtime.settings.unavailable)}</p>}
              {updatePreference.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.personal_runtime.settings.save_failed)}</p>}
            </div>
          )}
        </SettingsRow>
      </SettingsCard>
      {selectedId && preference.data && !failed && (
        <PersonalExecutionForm
          key={JSON.stringify([agent.id, preference.data, selectedRuntime?.provider])}
          preference={preference.data}
          runtime={selectedRuntime}
          inheritedLimit={agent.max_concurrent_tasks}
          disabled={updatePreference.isPending || unavailable}
          onSave={(value) => updatePreference.mutate(value)}
        />
      )}
    </SettingsSection>
  );
}

function PersonalExecutionForm({ preference, runtime, inheritedLimit, disabled, onSave }: {
  preference: AgentRuntimePreference;
  runtime?: AgentRuntime;
  inheritedLimit: number;
  disabled: boolean;
  onSave: (value: AgentRuntimePreference) => void;
}) {
  const { t } = useT("agents");
  const id = useId();
  const [mode, setMode] = useState<NonNullable<AgentRuntimePreference["modelMode"]>>(preference.modelMode === "custom" ? "custom" : "runtime_default");
  const [model, setModel] = useState(preference.model ?? "");
  const [limit, setLimit] = useState(preference.maxConcurrentTasks?.toString() ?? "");
  const models = useQuery(runtimeModelsOptions(mode === "custom" && runtime?.status === "online" ? runtime.id : null));
  const limitValue = limit.trim() === "" ? null : Number(limit);
  const valid = (mode !== "custom" || (model.trim().length > 0 && model.trim().length <= 256)) &&
    (limitValue === null || (Number.isInteger(limitValue) && limitValue >= 1 && limitValue <= 100));
  return (
    <form onSubmit={(event) => {
      event.preventDefault();
      if (valid && !disabled) onSave({ runtimeId: preference.runtimeId, modelMode: mode, model: mode === "custom" ? model.trim() : "", maxConcurrentTasks: limitValue });
    }}>
      <SettingsCard>
        <SettingsRow label={t(($) => $.personal_runtime.settings.model_mode)} size="select-wide">
          <Select items={{ runtime_default: t(($) => $.personal_runtime.settings.model_runtime_default), custom: t(($) => $.personal_runtime.settings.model_custom) }} value={mode} disabled={disabled} onValueChange={(value) => { if (value === "runtime_default" || value === "custom") setMode(value); }}>
            <SelectTrigger aria-label={t(($) => $.personal_runtime.settings.model_mode)}><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="runtime_default">{t(($) => $.personal_runtime.settings.model_runtime_default)}</SelectItem>
              <SelectItem value="custom">{t(($) => $.personal_runtime.settings.model_custom)}</SelectItem>
            </SelectContent>
          </Select>
        </SettingsRow>
        {mode === "custom" && (
          <SettingsRow label={t(($) => $.personal_runtime.settings.model_id)} size="select-wide">
            <div className="space-y-2">
              <Input aria-label={t(($) => $.personal_runtime.settings.model_id)} value={model} onChange={(event) => setModel(event.target.value)} maxLength={256} required disabled={disabled} list={id} />
              <datalist id={id}>{models.data?.models.map((entry) => <option key={entry.id} value={entry.id}>{entry.label}</option>)}</datalist>
              <p className="text-caption text-muted-foreground">{t(($) => $.personal_runtime.settings.model_hint)}</p>
            </div>
          </SettingsRow>
        )}
        <SettingsRow label={t(($) => $.personal_runtime.settings.concurrency)} size="select-wide">
          <div className="space-y-2">
            <Input type="number" min={1} max={100} step={1} aria-label={t(($) => $.personal_runtime.settings.concurrency)} value={limit} onChange={(event) => setLimit(event.target.value)} disabled={disabled} placeholder={t(($) => $.personal_runtime.settings.concurrency_inherit, { count: inheritedLimit })} />
            <p className="text-caption text-muted-foreground">{t(($) => $.personal_runtime.settings.concurrency_hint)}</p>
          </div>
        </SettingsRow>
        <div className="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-caption text-muted-foreground">{t(($) => $.personal_runtime.settings.execution_hint)}</p>
          <Button type="submit" className="shrink-0" disabled={disabled || !valid}>{t(($) => $.personal_runtime.settings.save_execution)}</Button>
        </div>
      </SettingsCard>
    </form>
  );
}

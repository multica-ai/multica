"use client";

import {
  useId,
  useMemo,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from "react";
import { Info, Loader2, Plus, Save, Trash2 } from "lucide-react";
import {
  useAgentBulkEditPresetsStore,
  type AgentBulkEditPreset,
  type AgentBulkEditPresetEnvOperation,
  type AgentBulkEditPresetPatch,
} from "@multica/core/agents/stores";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { createSafeId } from "@multica/core/utils";
import type {
  BulkAgentEnvKeySummary,
  BulkCustomArgOperation,
  BulkUpdateAgentsRequest,
  MemberWithUser,
  RuntimeDevice,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { splitCustomArgEntry } from "../custom-args";
import { RuntimePicker } from "./runtime-picker";
import { ModelDropdown } from "./model-dropdown";

interface EnvRow {
  id: string;
  action: "set" | "remove";
  key: string;
  value: string;
  needsPresetValue?: boolean;
}

type EnvOperation = { action: EnvRow["action"]; value: string };

interface CustomArgRow {
  id: string;
  action: "add" | "replace" | "remove";
  value: string;
  replacement: string;
}

interface BulkEditFormState {
  runtimeEnabled: boolean;
  runtimeId: string;
  modelEnabled: boolean;
  model: string;
  concurrencyEnabled: boolean;
  maxConcurrentTasks: string;
  customArgsEnabled: boolean;
  customArgRows: CustomArgRow[];
  envEnabled: boolean;
  envSetRows: EnvRow[];
}

export interface BulkCustomArgSummary {
  value: string;
  agentCount: number;
}

export function BulkEditAgentsDialog({
  title,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  onApply,
  onClose,
  affects,
  envKeyOptions = [],
  envKeysLoading = false,
  envKeysError = false,
  customArgOptions = [],
}: {
  title: string;
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  onApply: (request: BulkUpdateAgentsRequest) => Promise<void>;
  onClose: () => void;
  affects?: number;
  envKeyOptions?: BulkAgentEnvKeySummary[];
  envKeysLoading?: boolean;
  envKeysError?: boolean;
  customArgOptions?: BulkCustomArgSummary[];
}) {
  const { t } = useT("agents");
  const envKeyListId = useId();
  const customArgListId = useId();
  const presets = useAgentBulkEditPresetsStore((s) => s.presets);
  const savePreset = useAgentBulkEditPresetsStore((s) => s.savePreset);
  const removePreset = useAgentBulkEditPresetsStore((s) => s.removePreset);
  const [runtimeEnabled, setRuntimeEnabled] = useState(false);
  const [modelEnabled, setModelEnabled] = useState(false);
  const [concurrencyEnabled, setConcurrencyEnabled] = useState(false);
  const [customArgsEnabled, setCustomArgsEnabled] = useState(false);
  const [envEnabled, setEnvEnabled] = useState(false);
  const [runtimeId, setRuntimeId] = useState("");
  const [model, setModel] = useState("");
  const [maxConcurrentTasks, setMaxConcurrentTasks] = useState("1");
  const [customArgRows, setCustomArgRows] = useState<CustomArgRow[]>(() => [
    createCustomArgRow(),
  ]);
  const [envSetRows, setEnvSetRows] = useState<EnvRow[]>(() => [
    createEnvRow(),
  ]);
  const [presetName, setPresetName] = useState("");
  const [selectedPresetId, setSelectedPresetId] = useState("");
  const [busy, setBusy] = useState(false);

  const selectedRuntime = useMemo(
    () => runtimes.find((r) => r.id === runtimeId) ?? null,
    [runtimes, runtimeId],
  );

  const request = useMemo(
    () =>
      buildBulkUpdateRequest({
        runtimeEnabled,
        runtimeId,
        modelEnabled,
        model,
        concurrencyEnabled,
        maxConcurrentTasks,
        customArgsEnabled,
        customArgRows,
        envEnabled,
        envSetRows,
      }),
    [
      runtimeEnabled,
      runtimeId,
      modelEnabled,
      model,
      concurrencyEnabled,
      maxConcurrentTasks,
      customArgsEnabled,
      customArgRows,
      envEnabled,
      envSetRows,
    ],
  );
  const hasMissingPresetEnvValue = envEnabled && envSetRows.some((row) =>
    row.action === "set" && row.needsPresetValue && row.key.trim() && row.value === "",
  );
  const canApply = request !== null && !hasMissingPresetEnvValue;
  const presetPatch = useMemo(
    () =>
      buildPresetPatch({
        runtimeEnabled,
        runtimeId,
        modelEnabled,
        model,
        concurrencyEnabled,
        maxConcurrentTasks,
        customArgsEnabled,
        customArgRows,
        envEnabled,
        envSetRows,
      }),
    [
      runtimeEnabled,
      runtimeId,
      modelEnabled,
      model,
      concurrencyEnabled,
      maxConcurrentTasks,
      customArgsEnabled,
      customArgRows,
      envEnabled,
      envSetRows,
    ],
  );
  const selectedPreset =
    presets.find((preset) => preset.id === selectedPresetId) ?? null;
  const canSavePreset = presetPatch !== null && presetName.trim().length > 0;

  const apply = async () => {
    if (!request || hasMissingPresetEnvValue) return;
    setBusy(true);
    try {
      await onApply(request);
      onClose();
    } catch {
      // Keep the dialog open so the parent can surface the error and the user can retry.
    } finally {
      setBusy(false);
    }
  };

  const saveCurrentPreset = () => {
    if (!presetPatch || !presetName.trim()) return;
    const id = savePreset(presetName, presetPatch);
    setSelectedPresetId(id);
    setPresetName("");
  };

  const loadSelectedPreset = () => {
    if (!selectedPreset) return;
    applyPresetToForm(selectedPreset, {
      setRuntimeEnabled,
      setRuntimeId,
      setModelEnabled,
      setModel,
      setConcurrencyEnabled,
      setMaxConcurrentTasks,
      setCustomArgsEnabled,
      setCustomArgRows,
      setEnvEnabled,
      setEnvSetRows,
    });
  };

  const deleteSelectedPreset = () => {
    if (!selectedPreset) return;
    removePreset(selectedPreset.id);
    setSelectedPresetId("");
  };

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v && !busy) onClose();
      }}
    >
      <DialogContent
        className="!h-[85vh] !w-[calc(100vw-2rem)] !max-w-2xl p-0 gap-0 flex flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <DialogHeader className="shrink-0 border-b px-5 py-3 pr-10 space-y-0">
          <div className="flex min-w-0 items-center gap-2">
            <DialogTitle className="min-w-0 truncate text-base font-semibold">
              {title}
            </DialogTitle>
            <HelpButton
              label={t(($) => $.bulk_edit.help.dialog_aria)}
              help={t(($) => $.bulk_edit.help.dialog)}
              side="bottom"
            />
          </div>
          {affects != null ? (
            <p className="mt-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
              {t(($) => $.bulk_edit.affects, { count: affects })}
            </p>
          ) : null}
        </DialogHeader>
        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4">
          <div className="grid gap-3">
            <div className="rounded-lg border border-dashed border-border bg-muted/20 p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 space-y-1">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="text-sm font-medium">
                      {t(($) => $.bulk_edit.local_presets_title)}
                    </span>
                    <span className="rounded-md border bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
                      {t(($) => $.bulk_edit.local_presets_badge)}
                    </span>
                    <HelpButton
                      label={t(($) => $.bulk_edit.local_presets_help_aria)}
                      help={t(($) => $.bulk_edit.local_presets_help)}
                      side="bottom"
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.bulk_edit.local_presets_description)}
                  </p>
                </div>
              </div>
              <div className="mt-3 grid gap-2 md:grid-cols-[minmax(0,1fr)_auto_auto]">
                <Select
                  value={selectedPresetId}
                  onValueChange={(value) => setSelectedPresetId(value ?? "")}
                  disabled={presets.length === 0}
                >
                  <SelectTrigger
                    aria-label={t(($) => $.bulk_edit.local_preset_aria)}
                    size="sm"
                    className="h-8 w-full rounded-md text-xs"
                  >
                    <SelectValue
                      className={!selectedPreset ? "text-muted-foreground" : undefined}
                    >
                      {selectedPreset?.name ?? t(($) => $.bulk_edit.local_preset_placeholder)}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent align="start">
                    {presets.map((preset) => (
                      <SelectItem key={preset.id} value={preset.id}>
                        {preset.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={loadSelectedPreset}
                  disabled={!selectedPreset}
                >
                  {t(($) => $.bulk_edit.load_local_preset)}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={deleteSelectedPreset}
                  disabled={!selectedPreset}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  {t(($) => $.bulk_edit.delete_local_preset)}
                </Button>
              </div>
              <div className="mt-2 grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
                <Input
                  aria-label={t(($) => $.bulk_edit.preset_name_aria)}
                  value={presetName}
                  onChange={(e) => setPresetName(e.target.value)}
                  placeholder={t(($) => $.bulk_edit.preset_name_placeholder)}
                  className="h-8 text-xs"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={saveCurrentPreset}
                  disabled={!canSavePreset}
                >
                  <Save className="h-3.5 w-3.5" />
                  {t(($) => $.bulk_edit.save_local_preset)}
                </Button>
              </div>
            </div>

            <BulkField
              label={t(($) => $.bulk_edit.fields.runtime)}
              helpLabel={t(($) => $.bulk_edit.help.runtime_aria)}
              help={t(($) => $.bulk_edit.help.runtime)}
              checked={runtimeEnabled}
              onCheckedChange={(checked) => {
                setRuntimeEnabled(checked);
                if (!checked) setModel("");
              }}
            >
              <RuntimePicker
                runtimes={runtimes}
                runtimesLoading={runtimesLoading}
                members={members}
                currentUserId={currentUserId}
                selectedRuntimeId={runtimeId}
                onSelect={(id) => {
                  setRuntimeId(id);
                  setModel("");
                }}
              />
            </BulkField>

            <BulkField
              label={t(($) => $.bulk_edit.fields.model)}
              helpLabel={t(($) => $.bulk_edit.help.model_aria)}
              help={t(($) => $.bulk_edit.help.model)}
              checked={modelEnabled}
              onCheckedChange={setModelEnabled}
            >
              {runtimeEnabled ? (
                <ModelDropdown
                  runtimeId={selectedRuntime?.id ?? null}
                  runtimeOnline={selectedRuntime?.status === "online"}
                  value={model}
                  onChange={setModel}
                  disabled={!selectedRuntime}
                />
              ) : (
                <Input
                  aria-label={t(($) => $.bulk_edit.fields.model)}
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  placeholder={t(($) => $.bulk_edit.model_placeholder)}
                  className="font-mono text-xs"
                />
              )}
            </BulkField>

            <BulkField
              label={t(($) => $.bulk_edit.fields.max_concurrent_tasks)}
              helpLabel={t(($) => $.bulk_edit.help.max_concurrent_tasks_aria)}
              help={t(($) => $.bulk_edit.help.max_concurrent_tasks)}
              checked={concurrencyEnabled}
              onCheckedChange={setConcurrencyEnabled}
            >
              <Input
                aria-label={t(($) => $.bulk_edit.fields.max_concurrent_tasks)}
                type="number"
                min={1}
                max={50}
                value={maxConcurrentTasks}
                onChange={(e) => setMaxConcurrentTasks(e.target.value)}
              />
            </BulkField>

            <BulkField
              label={t(($) => $.bulk_edit.fields.custom_args)}
              helpLabel={t(($) => $.bulk_edit.help.custom_args_aria)}
              help={t(($) => $.bulk_edit.help.custom_args)}
              checked={customArgsEnabled}
              onCheckedChange={setCustomArgsEnabled}
            >
              <div className="grid gap-2">
                {customArgOptions.length > 0 ? (
                  <>
                    <datalist id={customArgListId}>
                      {customArgOptions.map((option) => (
                        <option
                          key={option.value}
                          value={option.value}
                          label={t(($) => $.bulk_edit.env_key_option_count, {
                            count: option.agentCount,
                          })}
                        />
                      ))}
                    </datalist>
                    <ExistingOptionPicker
                      title={t(($) => $.bulk_edit.existing_args)}
                      ariaLabel={t(($) => $.bulk_edit.existing_custom_arg_aria)}
                      placeholder={t(($) => $.bulk_edit.existing_custom_arg_placeholder)}
                      helpLabel={t(($) => $.bulk_edit.existing_args_help_aria)}
                      help={t(($) => $.bulk_edit.existing_args_help)}
                      options={customArgOptions.map((option) => ({
                        value: option.value,
                        count: t(($) => $.bulk_edit.env_key_option_count, {
                          count: option.agentCount,
                        }),
                        onSelect: () =>
                          insertCustomArgOption(setCustomArgRows, option.value),
                      }))}
                    />
                  </>
                ) : null}
                {customArgRows.map((row, index) => (
                  <div
                    key={row.id}
                    className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(8rem,0.65fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto]"
                  >
                    <Select
                      value={row.action}
                      onValueChange={(value) =>
                        updateCustomArgRow(
                          setCustomArgRows,
                          index,
                          "action",
                          normalizeCustomArgAction(value),
                        )
                      }
                    >
                      <SelectTrigger
                        aria-label={t(($) => $.bulk_edit.custom_arg_operation_aria)}
                        size="sm"
                        className="h-8 w-full rounded-md text-xs"
                      >
                        <SelectValue>
                          {row.action === "remove"
                            ? t(($) => $.bulk_edit.custom_arg_action_remove)
                            : row.action === "replace"
                              ? t(($) => $.bulk_edit.custom_arg_action_replace)
                              : t(($) => $.bulk_edit.custom_arg_action_add)}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent align="start">
                        <SelectItem value="add">
                          {t(($) => $.bulk_edit.custom_arg_action_add)}
                        </SelectItem>
                        <SelectItem value="replace">
                          {t(($) => $.bulk_edit.custom_arg_action_replace)}
                        </SelectItem>
                        <SelectItem value="remove">
                          {t(($) => $.bulk_edit.custom_arg_action_remove)}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    <Input
                      aria-label={
                        row.action === "remove"
                          ? t(($) => $.bulk_edit.custom_arg_remove_value_aria)
                          : row.action === "replace"
                            ? t(($) => $.bulk_edit.custom_arg_existing_value_aria)
                            : t(($) => $.bulk_edit.custom_arg_add_value_aria)
                      }
                      value={row.value}
                      onChange={(e) =>
                        updateCustomArgRow(
                          setCustomArgRows,
                          index,
                          "value",
                          e.target.value,
                        )
                      }
                      placeholder={t(($) => $.bulk_edit.custom_args_placeholder)}
                      list={customArgOptions.length > 0 ? customArgListId : undefined}
                      className="font-mono text-xs"
                    />
                    {row.action === "replace" ? (
                      <Input
                        aria-label={t(($) => $.bulk_edit.custom_arg_replacement_aria)}
                        value={row.replacement}
                        onChange={(e) =>
                          updateCustomArgRow(
                            setCustomArgRows,
                            index,
                            "replacement",
                            e.target.value,
                          )
                        }
                        placeholder={t(($) => $.bulk_edit.custom_arg_replacement_placeholder)}
                        className="font-mono text-xs"
                      />
                    ) : (
                      <Input
                        aria-label={t(($) => $.bulk_edit.custom_arg_replacement_unused_aria)}
                        value=""
                        disabled
                        placeholder={t(($) => $.bulk_edit.custom_arg_replacement_unused_placeholder)}
                        className="font-mono text-xs"
                      />
                    )}
                    <HelpButton
                      label={
                        row.action === "remove"
                          ? t(($) => $.bulk_edit.custom_arg_remove_help_aria)
                          : row.action === "replace"
                            ? t(($) => $.bulk_edit.custom_arg_replace_help_aria)
                            : t(($) => $.bulk_edit.custom_arg_add_help_aria)
                      }
                      help={
                        row.action === "remove"
                          ? t(($) => $.bulk_edit.custom_arg_remove_help)
                          : row.action === "replace"
                            ? t(($) => $.bulk_edit.custom_arg_replace_help)
                            : t(($) => $.bulk_edit.custom_arg_add_help)
                      }
                      side="top"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => removeCustomArgRow(setCustomArgRows, index)}
                      className="text-muted-foreground hover:text-destructive"
                      aria-label={t(($) => $.bulk_edit.remove_custom_arg)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    setCustomArgRows((rows) => [
                      ...rows,
                      createCustomArgRow(),
                    ])
                  }
                  className="justify-self-start"
                >
                  <Plus className="h-3 w-3" />
                  {t(($) => $.bulk_edit.add_custom_arg_operation)}
                </Button>
              </div>
            </BulkField>

            <BulkField
              label={t(($) => $.bulk_edit.fields.env)}
              helpLabel={t(($) => $.bulk_edit.help.env_aria)}
              help={t(($) => $.bulk_edit.help.env)}
              checked={envEnabled}
              onCheckedChange={setEnvEnabled}
            >
              <div className="grid gap-2">
                {envKeyOptions.length > 0 ? (
                  <datalist id={envKeyListId}>
                    {envKeyOptions.map((option) => (
                      <option
                        key={option.key}
                        value={option.key}
                        label={t(($) => $.bulk_edit.env_key_option_count, {
                          count: option.agent_count,
                        })}
                      />
                    ))}
                  </datalist>
                ) : null}
                {envKeysLoading && envKeyOptions.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.bulk_edit.env_keys_loading)}
                  </p>
                ) : envKeysError && envKeyOptions.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.bulk_edit.env_keys_load_failed)}
                  </p>
                ) : envKeyOptions.length > 0 ? (
                  <ExistingOptionPicker
                    title={t(($) => $.bulk_edit.existing_keys)}
                    ariaLabel={t(($) => $.bulk_edit.existing_env_key_aria)}
                    placeholder={t(($) => $.bulk_edit.existing_env_key_placeholder)}
                    helpLabel={t(($) => $.bulk_edit.existing_env_keys_help_aria)}
                    help={t(($) => $.bulk_edit.existing_env_keys_help)}
                    options={envKeyOptions.map((option) => ({
                      value: option.key,
                      count: t(($) => $.bulk_edit.env_key_option_count, {
                        count: option.agent_count,
                      }),
                      onSelect: () =>
                        insertEnvKeyOption(setEnvSetRows, option.key, "set"),
                    }))}
                  />
                ) : (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.bulk_edit.env_keys_empty)}
                  </p>
                )}
                {envSetRows.map((row, index) => (
                  <div
                    key={row.id}
                    className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(8rem,0.65fr)_minmax(0,1fr)_minmax(0,1fr)_auto_auto]"
                  >
                    <Select
                      value={row.action}
                      onValueChange={(value) =>
                        updateEnvSetRow(
                          setEnvSetRows,
                          index,
                          "action",
                          value === "remove" ? "remove" : "set",
                        )
                      }
                    >
                      <SelectTrigger
                        aria-label={t(($) => $.bulk_edit.env_operation_aria)}
                        size="sm"
                        className="h-8 w-full rounded-md text-xs"
                      >
                        <SelectValue>
                          {row.action === "remove"
                            ? t(($) => $.bulk_edit.env_action_remove)
                            : t(($) => $.bulk_edit.env_action_set)}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent align="start">
                        <SelectItem value="set">
                          {t(($) => $.bulk_edit.env_action_set)}
                        </SelectItem>
                        <SelectItem value="remove">
                          {t(($) => $.bulk_edit.env_action_remove)}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    <Input
                      aria-label={
                        row.action === "remove"
                          ? t(($) => $.bulk_edit.env_remove_key)
                          : t(($) => $.bulk_edit.env_set_key)
                      }
                      value={row.key}
                      onChange={(e) =>
                        updateEnvSetRow(
                          setEnvSetRows,
                          index,
                          "key",
                          e.target.value,
                        )
                      }
                      placeholder={t(($) => $.bulk_edit.env_key_placeholder)}
                      list={envKeyOptions.length > 0 ? envKeyListId : undefined}
                      className="font-mono text-xs"
                    />
                    {row.action === "set" ? (
                      <Input
                        aria-label={t(($) => $.bulk_edit.env_set_value)}
                        value={row.value}
                        onChange={(e) =>
                          updateEnvSetRow(
                            setEnvSetRows,
                            index,
                            "value",
                            e.target.value,
                          )
                        }
                        placeholder={t(($) => $.bulk_edit.env_value_placeholder)}
                        className="font-mono text-xs"
                      />
                    ) : (
                      <Input
                        aria-label={t(($) => $.bulk_edit.env_remove_value_unused)}
                        value=""
                        disabled
                        placeholder={t(($) => $.bulk_edit.env_value_unused_placeholder)}
                        className="font-mono text-xs"
                      />
                    )}
                    <div className="flex items-center gap-1 sm:contents">
                      <HelpButton
                        label={
                          row.action === "remove"
                            ? t(($) => $.bulk_edit.env_remove_help_aria)
                            : t(($) => $.bulk_edit.env_set_help_aria)
                        }
                        help={
                          row.action === "remove"
                            ? t(($) => $.bulk_edit.env_remove_help)
                            : t(($) => $.bulk_edit.env_set_help)
                        }
                        side="top"
                      />
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => removeEnvOperationRow(setEnvSetRows, index)}
                        className="text-muted-foreground hover:text-destructive"
                        aria-label={t(($) => $.bulk_edit.remove_env_operation)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                ))}
                {hasMissingPresetEnvValue ? (
                  <p className="rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">
                    {t(($) => $.bulk_edit.preset_env_values_required)}
                  </p>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    setEnvSetRows((rows) => [
                      ...rows,
                      createEnvRow(),
                    ])
                  }
                  className="justify-self-start"
                >
                  <Plus className="h-3 w-3" />
                  {t(($) => $.bulk_edit.add_env_operation)}
                </Button>
              </div>
            </BulkField>
          </div>
        </div>
        <div className="flex shrink-0 justify-end gap-2 border-t px-5 py-3">
          <Button variant="outline" onClick={onClose} disabled={busy}>
            {t(($) => $.bulk_edit.cancel)}
          </Button>
          <Button onClick={apply} disabled={busy || !canApply}>
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t(($) => $.bulk_edit.apply)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ExistingOptionPicker({
  title,
  ariaLabel,
  placeholder,
  helpLabel,
  help,
  options,
}: {
  title: string;
  ariaLabel: string;
  placeholder: string;
  helpLabel: string;
  help: string;
  options: Array<{
    value: string;
    count: string;
    onSelect: () => void;
  }>;
}) {
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center gap-2">
        <div className="text-xs font-medium text-muted-foreground">
          {title}
        </div>
        <HelpButton label={helpLabel} help={help} side="top" />
      </div>
      <Select
        value=""
        onValueChange={(value) => {
          const option = options.find((item) => item.value === value);
          option?.onSelect();
        }}
      >
        <SelectTrigger
          aria-label={ariaLabel}
          size="sm"
          className="h-8 w-full rounded-md text-xs"
        >
          <SelectValue className="text-muted-foreground">
            {placeholder}
          </SelectValue>
        </SelectTrigger>
        <SelectContent align="start" alignItemWithTrigger={false} className="max-h-72">
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              <code className="min-w-0 max-w-64 truncate font-mono">
                {option.value}
              </code>
              <span className="shrink-0 text-xs text-muted-foreground">
                {option.count}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function BulkField({
  label,
  helpLabel,
  help,
  checked,
  onCheckedChange,
  children,
}: {
  label: string;
  helpLabel: string;
  help: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  children: ReactNode;
}) {
  return (
    <div className="rounded-lg border border-border p-3">
      <div className="flex items-center justify-between gap-2">
        <label className="flex cursor-pointer items-center gap-2 text-sm font-medium">
          <Checkbox
            checked={checked}
            onCheckedChange={(value) => onCheckedChange(Boolean(value))}
          />
          <span>{label}</span>
        </label>
        <HelpButton label={helpLabel} help={help} />
      </div>
      {checked ? <div className="mt-3">{children}</div> : null}
    </div>
  );
}

function HelpButton({
  label,
  help,
  side = "left",
}: {
  label: string;
  help: string;
  side?: "top" | "right" | "bottom" | "left";
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={label}
            className="inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Info className="h-3.5 w-3.5" />
          </button>
        }
      />
      <TooltipContent side={side} className="max-w-80 leading-relaxed">
        {help}
      </TooltipContent>
    </Tooltip>
  );
}

function buildBulkUpdateRequest({
  runtimeEnabled,
  runtimeId,
  modelEnabled,
  model,
  concurrencyEnabled,
  maxConcurrentTasks,
  customArgsEnabled,
  customArgRows,
  envEnabled,
  envSetRows,
}: BulkEditFormState): BulkUpdateAgentsRequest | null {
  const request: BulkUpdateAgentsRequest = {};

  if (runtimeEnabled) {
    if (!runtimeId) return null;
    request.runtime_id = runtimeId;
  }
  if (modelEnabled) {
    request.model = model.trim();
  }
  if (concurrencyEnabled) {
    const parsed = parseMaxConcurrentTasks(maxConcurrentTasks);
    if (parsed == null) return null;
    request.max_concurrent_tasks = parsed;
  }
  if (customArgsEnabled) {
    const customArgsPatch = buildCustomArgsPatch(customArgRows);
    if (!customArgsPatch || customArgsPatch.length === 0) return null;
    request.custom_args_patch = customArgsPatch;
  }
  if (envEnabled) {
    const envPatch = buildEnvPatch(envSetRows);
    if (!envPatch) return null;
    if (envPatch.env_set) request.env_set = envPatch.env_set;
    if (envPatch.env_remove) request.env_remove = envPatch.env_remove;
  }

  return Object.keys(request).length > 0 ? request : null;
}

function buildPresetPatch({
  runtimeEnabled,
  runtimeId,
  modelEnabled,
  model,
  concurrencyEnabled,
  maxConcurrentTasks,
  customArgsEnabled,
  customArgRows,
  envEnabled,
  envSetRows,
}: BulkEditFormState): AgentBulkEditPresetPatch | null {
  const patch: AgentBulkEditPresetPatch = {};

  if (runtimeEnabled) {
    if (!runtimeId) return null;
    patch.runtimeId = runtimeId;
  }
  if (modelEnabled) patch.model = model.trim();
  if (concurrencyEnabled) {
    const parsed = parseMaxConcurrentTasks(maxConcurrentTasks);
    if (parsed == null) return null;
    patch.maxConcurrentTasks = parsed;
  }
  if (customArgsEnabled) {
    const customArgsPatch = buildCustomArgsPatch(customArgRows);
    if (!customArgsPatch || customArgsPatch.length === 0) return null;
    patch.customArgsPatch = customArgsPatch;
  }
  if (envEnabled) {
    const env = buildPresetEnv(envSetRows);
    if (!env) return null;
    patch.env = env;
  }

  return Object.keys(patch).length > 0 ? patch : null;
}

function parseMaxConcurrentTasks(value: string): number | null {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 50) return null;
  return parsed;
}

function collectEnvOperations(rows: EnvRow[]): Map<string, EnvOperation> {
  const envOps = new Map<string, EnvOperation>();
  for (const row of rows) {
    const key = row.key.trim();
    if (key) envOps.set(key, { action: row.action, value: row.value });
  }
  return envOps;
}

function buildEnvPatch(
  rows: EnvRow[],
): Pick<BulkUpdateAgentsRequest, "env_set" | "env_remove"> | null {
  const envOps = collectEnvOperations(rows);
  const envSet: Record<string, string> = {};
  const envRemove: string[] = [];

  for (const [key, op] of envOps) {
    if (op.action === "remove") {
      envRemove.push(key);
    } else {
      envSet[key] = op.value;
    }
  }

  const patch: Pick<BulkUpdateAgentsRequest, "env_set" | "env_remove"> = {};
  if (Object.keys(envSet).length > 0) patch.env_set = envSet;
  if (envRemove.length > 0) patch.env_remove = envRemove;
  return patch.env_set || patch.env_remove ? patch : null;
}

function buildPresetEnv(rows: EnvRow[]): AgentBulkEditPresetEnvOperation[] | undefined {
  const env = Array.from(collectEnvOperations(rows), ([key, op]) => ({
    action: op.action,
    key,
  }));
  return env.length > 0 ? env : undefined;
}

function applyPresetToForm(
  preset: AgentBulkEditPreset,
  setters: {
    setRuntimeEnabled: Dispatch<SetStateAction<boolean>>;
    setRuntimeId: Dispatch<SetStateAction<string>>;
    setModelEnabled: Dispatch<SetStateAction<boolean>>;
    setModel: Dispatch<SetStateAction<string>>;
    setConcurrencyEnabled: Dispatch<SetStateAction<boolean>>;
    setMaxConcurrentTasks: Dispatch<SetStateAction<string>>;
    setCustomArgsEnabled: Dispatch<SetStateAction<boolean>>;
    setCustomArgRows: Dispatch<SetStateAction<CustomArgRow[]>>;
    setEnvEnabled: Dispatch<SetStateAction<boolean>>;
    setEnvSetRows: Dispatch<SetStateAction<EnvRow[]>>;
  },
) {
  const { patch } = preset;

  setters.setRuntimeEnabled(patch.runtimeId !== undefined);
  setters.setRuntimeId(patch.runtimeId ?? "");
  setters.setModelEnabled(patch.model !== undefined);
  setters.setModel(patch.model ?? "");
  setters.setConcurrencyEnabled(patch.maxConcurrentTasks !== undefined);
  setters.setMaxConcurrentTasks(String(patch.maxConcurrentTasks ?? 1));
  setters.setCustomArgsEnabled(
    patch.customArgsPatch !== undefined,
  );
  setters.setCustomArgRows(() => {
    if (patch.customArgsPatch && patch.customArgsPatch.length > 0) {
      return patch.customArgsPatch.map((op) =>
        createCustomArgRow(op.action, op.value, op.replacement ?? ""),
      );
    }
    return [createCustomArgRow()];
  });
  setters.setEnvEnabled(patch.env !== undefined);
  setters.setEnvSetRows(
    patch.env && patch.env.length > 0
      ? patch.env.map((op) =>
          createEnvRow(op.action, op.key, "", op.action === "set"),
        )
      : [createEnvRow()],
  );
}

function createCustomArgRow(
  action: CustomArgRow["action"] = "add",
  value = "",
  replacement = "",
): CustomArgRow {
  return { id: createSafeId(), action, value, replacement };
}

function createEnvRow(
  action: EnvRow["action"] = "set",
  key = "",
  value = "",
  needsPresetValue = false,
): EnvRow {
  return {
    id: createSafeId(),
    action,
    key,
    value,
    ...(needsPresetValue ? { needsPresetValue } : {}),
  };
}

function insertCustomArgOption(
  setRows: Dispatch<SetStateAction<CustomArgRow[]>>,
  value: string,
) {
  setRows((rows) => {
    const emptyIndex = rows.findIndex((row) => row.value.trim() === "");
    if (emptyIndex >= 0) {
      return rows.map((row, i) =>
        i === emptyIndex
          ? { ...row, action: "replace", value, replacement: "" }
          : row,
      );
    }
    return [...rows, createCustomArgRow("replace", value)];
  });
}

function insertEnvKeyOption(
  setRows: Dispatch<SetStateAction<EnvRow[]>>,
  key: string,
  action: "set" | "remove",
) {
  setRows((rows) => {
    const emptyIndex = rows.findIndex((row) => row.key.trim() === "");
    if (emptyIndex >= 0) {
      return rows.map((row, i) =>
        i === emptyIndex
          ? {
              ...row,
              action,
              key,
              value: action === "remove" ? "" : row.value,
              needsPresetValue: false,
            }
          : row,
      );
    }
    return [...rows, createEnvRow(action, key)];
  });
}

function updateEnvSetRow(
  setRows: Dispatch<SetStateAction<EnvRow[]>>,
  index: number,
  field: "action" | "key" | "value",
  value: string,
) {
  setRows((rows) =>
    rows.map((row, i) => {
      if (i !== index) return row;
      const next = { ...row, [field]: value };
      if (field === "value" || (field === "action" && value === "remove")) {
        delete next.needsPresetValue;
      }
      return next;
    }),
  );
}

function removeEnvOperationRow(
  setRows: Dispatch<SetStateAction<EnvRow[]>>,
  index: number,
) {
  setRows((rows) => rows.filter((_, i) => i !== index));
}

function updateCustomArgRow(
  setRows: Dispatch<SetStateAction<CustomArgRow[]>>,
  index: number,
  field: "action" | "value" | "replacement",
  value: string,
) {
  setRows((rows) =>
    rows.map((row, i) => (i === index ? { ...row, [field]: value } : row)),
  );
}

function removeCustomArgRow(
  setRows: Dispatch<SetStateAction<CustomArgRow[]>>,
  index: number,
) {
  setRows((rows) => rows.filter((_, i) => i !== index));
}

function normalizeCustomArgAction(value: string | null): CustomArgRow["action"] {
  if (value === "replace" || value === "remove") return value;
  return "add";
}

function buildCustomArgsPatch(rows: CustomArgRow[]): BulkCustomArgOperation[] | null {
  const operations: BulkCustomArgOperation[] = [];
  for (const row of rows) {
    const action = normalizeCustomArgAction(row.action);
    const values =
      action === "add" ? splitCustomArgEntry(row.value) : [row.value.trim()];
    const value = values[0] ?? "";
    const replacement = row.replacement.trim();
    if (!value) {
      if (replacement) return null;
      continue;
    }
    if (action === "replace") {
      if (!replacement) return null;
      operations.push({ action, value, replacement });
      continue;
    }
    for (const nextValue of values) {
      operations.push({ action, value: nextValue });
    }
  }
  return operations;
}

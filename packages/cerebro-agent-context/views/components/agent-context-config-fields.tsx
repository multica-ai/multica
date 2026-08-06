"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime, RuntimeModel } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  runtimeListOptions,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import { getAgentCapabilities } from "@multica/cerebro-agent-capabilities";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import type { AgentInstructionsEditor } from "../context-tab";

const CUSTOM = "__custom__";

interface Props {
  agent: Agent;
  runtimeId: string;
  model: string;
  thinkingLevel: string;
  workspaceBriefMode: string;
  toolsBriefMode: string;
  systemPromptMode: string;
  speedMode: string;
  maxTurns: string;
  timeoutMinutes: string;
  instructions?: string;
  busy?: boolean;
  controlFirst?: boolean;
  instructionsEditor?: AgentInstructionsEditor;
  editorKey?: number;
  onInstructions?: (v: string) => void;
  onRuntimeId: (v: string) => void;
  onModel: (v: string) => void;
  onThinkingLevel: (v: string) => void;
  onWorkspaceBriefMode: (v: string) => void;
  onToolsBriefMode: (v: string) => void;
  onSystemPromptMode: (v: string) => void;
  onSpeedMode: (v: string) => void;
  onMaxTurns: (v: string) => void;
  onTimeoutMinutes: (v: string) => void;
}

export function AgentContextConfigFields(props: Props) {
  const {
    agent,
    model,
    thinkingLevel,
  } = props;
  const wsId = useWorkspaceId();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes.find((item) => item.id === props.runtimeId) ?? null;
  const runtimeOnline = runtime?.status === "online";
  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? props.runtimeId : null),
  );
  const capabilityQuery = useQuery({
    queryKey: ["agent-capabilities", agent.id],
    queryFn: () => getAgentCapabilities(agent.id),
    enabled: props.controlFirst === true,
  });
  const options = capabilityQuery.data?.runtime_options;
  const models = useMemo(
    () => modelsQuery.data?.models ?? [],
    [modelsQuery.data],
  );
  const supported = modelsQuery.data?.supported ?? true;
  const hasModelCatalog = supported && models.length > 0;
  const modelInList = models.some((entry) => entry.id === model);
  const [modelCustom, setModelCustom] = useState(
    () => model !== "" && hasModelCatalog && !modelInList,
  );
  const thinkingEntry = pickModelEntry(models, model);
  const levels = thinkingEntry?.thinking?.supported_levels ?? [];
  const thinkingInList = levels.some((level) => level.value === thinkingLevel);

  const ignored = (field: string) => {
    if (options?.status !== "known") return false;
    const row = options.exec_options.find((item) => item.field === field);
    return row != null && !row.effective;
  };
  const systemModes =
    options?.status === "known" && options.system_prompt
      ? options.system_prompt.modes
      : ["append", "replace", "prepend"];

  const shared = {
    ...props,
    runtimes,
    runtimeName: runtime?.name ?? options?.provider ?? "Current engine",
    models,
    hasModelCatalog,
    modelInList,
    modelCustom,
    setModelCustom,
    levels,
    thinkingInList,
    modelsLoading: modelsQuery.isLoading,
    runtimeOnline,
    systemModes,
    ignored,
  };

  if (!props.controlFirst) {
    return <LegacyFields {...shared} />;
  }
  return <ControlFirstFields {...shared} />;
}

type SharedProps = Props & {
  runtimeName: string;
  runtimes: AgentRuntime[];
  models: RuntimeModel[];
  hasModelCatalog: boolean;
  modelInList: boolean;
  modelCustom: boolean;
  setModelCustom: (value: boolean) => void;
  levels: Array<{ value: string; label: string }>;
  thinkingInList: boolean;
  modelsLoading: boolean;
  runtimeOnline: boolean;
  systemModes: string[];
  ignored: (field: string) => boolean;
};

function ControlFirstFields(props: SharedProps) {
  const {
    instructions = "",
    instructionsEditor: InstructionsEditor,
    editorKey = 0,
    onInstructions,
    busy = false,
    workspaceBriefMode,
    toolsBriefMode,
    systemPromptMode,
    speedMode,
    maxTurns,
    timeoutMinutes,
    runtimeId,
    onWorkspaceBriefMode,
    onToolsBriefMode,
    onSystemPromptMode,
    onSpeedMode,
    onMaxTurns,
    onTimeoutMinutes,
    onRuntimeId,
    systemModes,
    ignored,
  } = props;
  const keepsEngineInstructions = systemPromptMode !== "replace";

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <ControlSectionLabel>Who this agent is</ControlSectionLabel>
      <div className="border-b px-4 pb-4 pt-2 md:px-5">
        <ControlField
          label="Instructions"
          htmlFor="agent-instructions"
          helper={`${instructions.length.toLocaleString("en-US")} characters in this agent's own role and boundaries.`}
        >
          {InstructionsEditor ? (
            <div
              id="agent-instructions"
              role="region"
              aria-label="Instructions"
              aria-disabled={busy}
              className={`min-h-48 rounded-md border bg-background ${
                busy ? "pointer-events-none opacity-60" : ""
              }`}
            >
              <InstructionsEditor
                key={`${props.agent.id}:${editorKey}`}
                defaultValue={instructions}
                onUpdate={onInstructions}
                placeholder="Write what this agent should do, how it should respond, and what it must never do…"
                className="min-h-48 px-4 py-3 text-sm"
                debounceMs={0}
                disableMentions
              />
            </div>
          ) : (
            <Textarea
              id="agent-instructions"
              aria-label="Instructions"
              value={instructions}
              onChange={(event) => onInstructions?.(event.target.value)}
              readOnly={busy}
              rows={10}
              className="min-h-48 resize-y bg-background text-sm leading-relaxed"
            />
          )}
        </ControlField>
      </div>
      <ControlRow
        label="Engine's own instructions"
        helper="Keep the engine's general instructions, or drop them so only this agent's role shapes its work."
        consequence={
          keepsEngineInstructions
            ? "Adds engine text before the task"
            : "Only this agent's role is used"
        }
        ignored={ignored("system_prompt")}
      >
        <ChoiceGroup
          value={keepsEngineInstructions ? "keep" : "drop"}
          disabled={ignored("system_prompt") || systemModes.length === 0}
          options={[
            {
              value: "keep",
              label: "Keep them",
              onSelect: () =>
                onSystemPromptMode(
                  systemModes.includes("append") ? "append" : "",
                ),
            },
            {
              value: "drop",
              label: "Drop them",
              onSelect: () => onSystemPromptMode("replace"),
            },
          ]}
        />
      </ControlRow>

      <ControlSectionLabel>What this agent reads before a task</ControlSectionLabel>
      <ControlRow
        id="ac-workspace-brief"
        label="The shared workspace brief"
        helper="Workspace rules, product context and working conventions."
        consequence={
          workspaceBriefMode === "off"
            ? "Removed from the next prompt"
            : "Its full text is added"
        }
      >
        <ChoiceGroup
          value={workspaceBriefMode === "off" ? "skip" : "read"}
          options={[
            {
              value: "read",
              label: "Reads it",
              onSelect: () => onWorkspaceBriefMode(""),
            },
            {
              value: "skip",
              label: "Skips it",
              onSelect: () => onWorkspaceBriefMode("off"),
            },
          ]}
        />
      </ControlRow>
      <ControlRow
        id="ac-tools-brief"
        label="Its tool list"
        helper="Only the wording changes. The agent keeps access to every tool either way."
        consequence={
          toolsBriefMode === "summary"
            ? "Shorter text; access is unchanged"
            : "Every tool description is added"
        }
      >
        <ChoiceGroup
          value={toolsBriefMode === "summary" ? "summary" : "full"}
          options={[
            {
              value: "full",
              label: "Every tool",
              onSelect: () => onToolsBriefMode(""),
            },
            {
              value: "summary",
              label: "One line per connection",
              onSelect: () => onToolsBriefMode("summary"),
            },
          ]}
        />
      </ControlRow>
      <div className="flex flex-wrap items-center gap-2 border-b bg-muted/40 px-4 py-3 text-xs md:px-5">
        <span className="font-medium">
          The exact total before the task is recorded after every run.
        </span>
        <a
          className="ml-auto underline underline-offset-2"
          href="?tab=production_prompt"
        >
          See the whole thing →
        </a>
      </div>

      <ControlSectionLabel id="how-agent-runs">
        How this agent runs
      </ControlSectionLabel>
      <div className="divide-y border-b">
        <div className="px-4 py-3 md:px-5">
          <ModelControl {...props} row />
        </div>
        <div className="px-4 py-3 md:px-5">
          <ThinkingControl {...props} row />
        </div>
        <ControlRow
          label="Speed"
          helper="Fast answers with the same model. It is only used where the model supports it."
          ignored={ignored("speed_mode")}
        >
          <ChoiceGroup
            value={speedMode === "fast" ? "fast" : "standard"}
            disabled={ignored("speed_mode")}
            options={[
              {
                value: "standard",
                label: "Standard",
                onSelect: () => onSpeedMode(""),
              },
              {
                value: "fast",
                label: "Fast",
                onSelect: () => onSpeedMode("fast"),
              },
            ]}
          />
        </ControlRow>
        <ControlRow
          label="Stop after"
          helper="Stops a run that keeps taking steps without finishing."
          ignored={ignored("max_turns")}
        >
          <div className="flex items-center gap-2">
            <Input
              id="ac-max-turns"
              aria-label="Stop after"
              type="number"
              min={1}
              step={1}
              value={maxTurns}
              onChange={(event) => onMaxTurns(event.target.value)}
              placeholder="Default"
              disabled={ignored("max_turns")}
              className="w-28 text-right font-mono text-xs"
            />
            <span className="text-xs text-muted-foreground">steps</span>
          </div>
        </ControlRow>
        <ControlRow
          label="Give up after"
          helper="Sets the hard time limit for one run."
          ignored={ignored("timeout_minutes")}
        >
          <div className="flex items-center gap-2">
            <Input
              id="ac-timeout-minutes"
              aria-label="Give up after"
              type="number"
              min={1}
              step={1}
              value={timeoutMinutes}
              onChange={(event) => onTimeoutMinutes(event.target.value)}
              placeholder="Default"
              disabled={ignored("timeout_minutes")}
              className="w-28 text-right font-mono text-xs"
            />
            <span className="text-xs text-muted-foreground">minutes</span>
          </div>
        </ControlRow>
        <ControlRow
          label="Engine"
          helper="Any loss of supported settings is shown before this change is approved."
        >
          <NativeSelect
            id="ac-runtime"
            aria-label="Engine"
            value={runtimeId}
            onChange={(event) => onRuntimeId(event.target.value)}
            className="w-full min-w-56"
          >
            {props.runtimes.map((item) => (
              <NativeSelectOption key={item.id} value={item.id}>
                {item.custom_name || item.name} · {item.provider}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </ControlRow>
      </div>

      <div className="bg-muted/40 px-4 py-3 text-xs text-muted-foreground md:px-5">
        <span className="font-medium text-foreground">
          Not on this screen, on purpose.
        </span>{" "}
        <a className="underline underline-offset-2" href="?tab=tools">
          Tools
        </a>
        ,{" "}
        <a className="underline underline-offset-2" href="?tab=skills">
          Skills
        </a>
        ,{" "}
        <a className="underline underline-offset-2" href="?tab=env">
          Secrets
        </a>
        ,{" "}
        <a className="underline underline-offset-2" href="?tab=infisical">
          Infisical
        </a>
        ,{" "}
        <a className="underline underline-offset-2" href="?tab=mcp_config">
          MCP
        </a>
        , and{" "}
        <a className="underline underline-offset-2" href="?tab=custom_args">
          Custom args
        </a>{" "}
        stay in their own tabs.
      </div>
    </div>
  );
}

function LegacyFields(props: SharedProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <ModelControl {...props} />
      <ThinkingControl {...props} />
      <ControlField label="Workspace brief" htmlFor="ac-workspace-brief">
        <NativeSelect
          id="ac-workspace-brief"
          value={props.workspaceBriefMode}
          onChange={(event) => props.onWorkspaceBriefMode(event.target.value)}
          className="w-full"
        >
          <NativeSelectOption value="">Full (default)</NativeSelectOption>
          <NativeSelectOption value="off">Off — only this agent&apos;s role</NativeSelectOption>
        </NativeSelect>
      </ControlField>
      <ControlField label="Tools list" htmlFor="ac-tools-brief">
        <NativeSelect
          id="ac-tools-brief"
          value={props.toolsBriefMode}
          onChange={(event) => props.onToolsBriefMode(event.target.value)}
          className="w-full"
        >
          <NativeSelectOption value="">Full list (default)</NativeSelectOption>
          <NativeSelectOption value="summary">Summary — one line per connection</NativeSelectOption>
        </NativeSelect>
      </ControlField>
      <ControlField label="Engine system prompt" htmlFor="ac-system-prompt-mode">
        <NativeSelect
          id="ac-system-prompt-mode"
          value={props.systemPromptMode}
          onChange={(event) => props.onSystemPromptMode(event.target.value)}
          className="w-full"
        >
          <NativeSelectOption value="">Engine default</NativeSelectOption>
          <NativeSelectOption value="append">Append</NativeSelectOption>
          <NativeSelectOption value="replace">Replace</NativeSelectOption>
          <NativeSelectOption value="prepend">Prepend</NativeSelectOption>
        </NativeSelect>
      </ControlField>
    </div>
  );
}

function ModelControl(props: SharedProps & { row?: boolean }) {
  const {
    model,
    models,
    hasModelCatalog,
    modelInList,
    modelCustom,
    setModelCustom,
    onModel,
    modelsLoading,
    runtimeOnline,
    ignored,
  } = props;
  return (
    <ControlField
      label="Model"
      htmlFor="ac-model"
      helper={
        !hasModelCatalog && !modelsLoading
          ? runtimeOnline
            ? "No model catalog was published."
            : "Engine offline — enter a model id."
          : undefined
      }
      ignored={ignored("model")}
      row={props.row}
    >
      {hasModelCatalog && !modelCustom ? (
        <NativeSelect
          id="ac-model"
          value={modelInList ? model : model === "" ? "" : CUSTOM}
          onChange={(event) => {
            if (event.target.value === CUSTOM) {
              setModelCustom(true);
              return;
            }
            onModel(event.target.value);
          }}
          className="w-full"
          disabled={ignored("model")}
        >
          <NativeSelectOption value="">Engine default</NativeSelectOption>
          {models.map((entry) => (
            <NativeSelectOption key={entry.id} value={entry.id}>
              {entry.label}
            </NativeSelectOption>
          ))}
          <NativeSelectOption value={CUSTOM}>Custom…</NativeSelectOption>
        </NativeSelect>
      ) : (
        <Input
          id="ac-model"
          aria-label="Model"
          value={model}
          onChange={(event) => onModel(event.target.value)}
          placeholder="Engine default"
          disabled={ignored("model")}
          className="font-mono text-xs"
        />
      )}
    </ControlField>
  );
}

function ThinkingControl(props: SharedProps & { row?: boolean }) {
  const {
    thinkingLevel,
    levels,
    thinkingInList,
    onThinkingLevel,
    ignored,
  } = props;
  return (
    <ControlField
      label="Thinking time"
      htmlFor="ac-thinking"
      helper={
        props.controlFirst
          ? "Higher effort can improve difficult work, but takes longer."
          : "Values come from the selected model."
      }
      ignored={ignored("thinking_level")}
      row={props.row}
    >
      {levels.length > 0 ? (
        <NativeSelect
          id="ac-thinking"
          value={thinkingLevel}
          onChange={(event) => onThinkingLevel(event.target.value)}
          className="w-full"
          disabled={ignored("thinking_level")}
        >
          <NativeSelectOption value="">Engine default</NativeSelectOption>
          {levels.map((level) => (
            <NativeSelectOption key={level.value} value={level.value}>
              {level.label}
            </NativeSelectOption>
          ))}
          {thinkingLevel !== "" && !thinkingInList && (
            <NativeSelectOption value={thinkingLevel}>
              {thinkingLevel}
            </NativeSelectOption>
          )}
        </NativeSelect>
      ) : (
        <Input
          id="ac-thinking"
          aria-label="Thinking time"
          value={thinkingLevel}
          onChange={(event) => onThinkingLevel(event.target.value)}
          placeholder="Engine default"
          disabled={ignored("thinking_level")}
          className="font-mono text-xs"
        />
      )}
    </ControlField>
  );
}

function ControlSectionLabel({
  id,
  children,
}: {
  id?: string;
  children: React.ReactNode;
}) {
  return (
    <h4
      id={id}
      className="scroll-mt-4 border-b px-4 pb-2 pt-4 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground md:px-5"
    >
      {children}
    </h4>
  );
}

function ControlRow({
  id,
  label,
  helper,
  consequence,
  ignored = false,
  children,
}: {
  id?: string;
  label: string;
  helper?: string;
  consequence?: string;
  ignored?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      id={id}
      className="grid scroll-mt-4 gap-3 border-b px-4 py-3 md:grid-cols-[minmax(0,1fr)_minmax(16rem,auto)] md:items-center md:px-5"
    >
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">{label}</span>
          {ignored && <Badge variant="outline">Ignored by engine</Badge>}
        </div>
        {helper && (
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {helper}
          </p>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2 md:justify-end">
        {children}
        {consequence && (
          <span className="text-xs font-medium text-emerald-700 dark:text-emerald-300">
            {consequence}
          </span>
        )}
      </div>
    </div>
  );
}

function ChoiceGroup({
  value,
  options,
  disabled = false,
}: {
  value: string;
  options: Array<{ value: string; label: string; onSelect: () => void }>;
  disabled?: boolean;
}) {
  return (
    <div className="inline-flex overflow-hidden rounded-md border bg-muted/40">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={value === option.value}
          disabled={disabled}
          onClick={option.onSelect}
          className={`border-r px-3 py-2 text-xs last:border-r-0 disabled:cursor-not-allowed disabled:opacity-50 ${
            value === option.value
              ? "bg-background font-semibold shadow-sm"
              : "text-muted-foreground hover:bg-background/70"
          }`}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function ControlField({
  label,
  htmlFor,
  helper,
  ignored = false,
  row = false,
  children,
}: {
  label: string;
  htmlFor?: string;
  helper?: string;
  ignored?: boolean;
  row?: boolean;
  children: React.ReactNode;
}) {
  if (row) {
    return (
      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(16rem,22rem)] md:items-center">
        <div>
          <div className="flex min-h-5 items-center gap-2">
            <Label htmlFor={htmlFor} className="text-sm">
              {label}
            </Label>
            {ignored && <Badge variant="outline">Ignored by engine</Badge>}
          </div>
          {helper && (
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {helper}
            </p>
          )}
        </div>
        <div className="md:justify-self-end md:w-full">{children}</div>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      <div className="flex min-h-5 items-center justify-between gap-2">
        <Label htmlFor={htmlFor} className="text-xs">{label}</Label>
        {ignored && <Badge variant="outline">Ignored by engine</Badge>}
      </div>
      {children}
      {helper && <p className="text-xs leading-relaxed text-muted-foreground">{helper}</p>}
    </div>
  );
}

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
): RuntimeModel | undefined {
  if (model) return models.find((entry) => entry.id === model);
  return models.find((entry) => entry.default) ?? models[0];
}

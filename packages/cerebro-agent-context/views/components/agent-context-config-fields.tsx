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
  const tokenEstimate = Math.ceil(instructions.length / 4);

  return (
    <div className="space-y-4">
      <ControlGroup eyebrow="Who she is">
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_21rem]">
          <ControlField
            label="Instructions"
            htmlFor="agent-instructions"
            helper={`${instructions.length.toLocaleString("en-US")} characters · ~${tokenEstimate.toLocaleString("en-US")} tokens`}
          >
            <Textarea
              id="agent-instructions"
              aria-label="Instructions"
              value={instructions}
              onChange={(event) => onInstructions?.(event.target.value)}
              readOnly={busy}
              rows={16}
              className="min-h-64 resize-y bg-background font-mono text-xs leading-relaxed"
            />
          </ControlField>

          <ControlField
            label="Engine system prompt"
            htmlFor="ac-system-prompt-mode"
            helper="Choose whether the engine keeps its own instruction beside this agent's role."
            ignored={ignored("system_prompt")}
          >
            <NativeSelect
              id="ac-system-prompt-mode"
              value={systemPromptMode}
              onChange={(event) => onSystemPromptMode(event.target.value)}
              className="w-full"
              disabled={systemModes.length === 0}
            >
              <NativeSelectOption value="">Engine default</NativeSelectOption>
              {systemModes.includes("append") && (
                <NativeSelectOption value="append">
                  Append — keep the engine prompt
                </NativeSelectOption>
              )}
              {systemModes.includes("replace") && (
                <NativeSelectOption value="replace">
                  Replace — use this agent&apos;s role
                </NativeSelectOption>
              )}
              {systemModes.includes("prepend") && (
                <NativeSelectOption value="prepend">
                  Prepend — send as user text
                </NativeSelectOption>
              )}
            </NativeSelect>
          </ControlField>
        </div>
      </ControlGroup>

      <ControlGroup eyebrow="What she reads">
        <div className="grid gap-4 md:grid-cols-2">
          <ControlField
            label="Shared brief"
            htmlFor="ac-workspace-brief"
            helper="The common Multica commands, rules and workspace context."
          >
            <NativeSelect
              id="ac-workspace-brief"
              value={workspaceBriefMode}
              onChange={(event) => onWorkspaceBriefMode(event.target.value)}
              className="w-full"
            >
              <NativeSelectOption value="">Read full brief</NativeSelectOption>
              <NativeSelectOption value="off">
                Skip — use only this agent&apos;s role
              </NativeSelectOption>
            </NativeSelect>
          </ControlField>

          <ControlField
            label="Tools list"
            htmlFor="ac-tools-brief"
            helper="The resolved tools and connections available for the run."
          >
            <NativeSelect
              id="ac-tools-brief"
              value={toolsBriefMode}
              onChange={(event) => onToolsBriefMode(event.target.value)}
              className="w-full"
            >
              <NativeSelectOption value="">Read full list</NativeSelectOption>
              <NativeSelectOption value="summary">
                Read one-line connection summary
              </NativeSelectOption>
            </NativeSelect>
          </ControlField>
        </div>
      </ControlGroup>

      <ControlGroup eyebrow="How she runs">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
          <ControlField
            label="Engine"
            htmlFor="ac-runtime"
            helper="Where this agent runs. Changing it is reviewed with the other settings."
          >
            <NativeSelect
              id="ac-runtime"
              aria-label="Engine"
              value={runtimeId}
              onChange={(event) => onRuntimeId(event.target.value)}
              className="w-full"
            >
              {props.runtimes.map((item) => (
                <NativeSelectOption key={item.id} value={item.id}>
                  {item.custom_name || item.name} · {item.provider}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </ControlField>
          <ModelControl {...props} />
          <ThinkingControl {...props} />
          <ControlField
            label="Speed"
            htmlFor="ac-speed"
            helper="Standard inherits the engine's normal response tier."
            ignored={ignored("speed_mode")}
          >
            <NativeSelect
              id="ac-speed"
              value={speedMode}
              onChange={(event) => onSpeedMode(event.target.value)}
              className="w-full"
              disabled={ignored("speed_mode")}
            >
              <NativeSelectOption value="">Standard</NativeSelectOption>
              <NativeSelectOption value="fast">Fast</NativeSelectOption>
            </NativeSelect>
          </ControlField>
          <ControlField
            label="Stop after"
            htmlFor="ac-max-turns"
            helper="Agent turns. Blank inherits the selected run mode."
            ignored={ignored("max_turns")}
          >
            <Input
              id="ac-max-turns"
              aria-label="Stop after"
              type="number"
              min={1}
              step={1}
              value={maxTurns}
              onChange={(event) => onMaxTurns(event.target.value)}
              placeholder="Mode default"
              disabled={ignored("max_turns")}
              className="font-mono text-xs"
            />
          </ControlField>
          <ControlField
            label="Give up after"
            htmlFor="ac-timeout-minutes"
            helper="Minutes. Blank inherits the selected run mode."
            ignored={ignored("timeout_minutes")}
          >
            <Input
              id="ac-timeout-minutes"
              aria-label="Give up after"
              type="number"
              min={1}
              step={1}
              value={timeoutMinutes}
              onChange={(event) => onTimeoutMinutes(event.target.value)}
              placeholder="Mode default"
              disabled={ignored("timeout_minutes")}
              className="font-mono text-xs"
            />
          </ControlField>
        </div>
      </ControlGroup>

      <div className="rounded-lg border border-dashed px-4 py-3 text-xs text-muted-foreground">
        <span className="font-medium text-foreground">Managed elsewhere:</span>{" "}
        <a className="underline underline-offset-2" href="?tab=tools">Tools</a>,{" "}
        <a className="underline underline-offset-2" href="?tab=skills">Skills</a>,{" "}
        <a className="underline underline-offset-2" href="?tab=env">Secrets</a>,{" "}
        <a className="underline underline-offset-2" href="?tab=infisical">Infisical</a>,{" "}
        <a className="underline underline-offset-2" href="?tab=mcp_config">MCP</a>, and{" "}
        <a className="underline underline-offset-2" href="?tab=custom_args">Custom args</a>.
        Each setting has one home.
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

function ModelControl(props: SharedProps) {
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

function ThinkingControl(props: SharedProps) {
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
      helper="Values come from the selected model."
      ignored={ignored("thinking_level")}
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

function ControlGroup({
  eyebrow,
  children,
}: {
  eyebrow: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card p-4">
      <p className="mb-4 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {eyebrow}
      </p>
      {children}
    </section>
  );
}

function ControlField({
  label,
  htmlFor,
  helper,
  ignored = false,
  children,
}: {
  label: string;
  htmlFor?: string;
  helper?: string;
  ignored?: boolean;
  children: React.ReactNode;
}) {
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

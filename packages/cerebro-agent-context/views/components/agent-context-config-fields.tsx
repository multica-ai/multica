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
  const tokenEstimate = Math.ceil(instructions.length / 4);
  const workspaceSummary =
    workspaceBriefMode === "off"
      ? "Uses this agent's role only"
      : "Uses the shared workspace guidance";
  const toolsSummary =
    toolsBriefMode === "summary"
      ? "Reads short connection summaries"
      : "Reads full tool descriptions";
  const runSummary = [
    props.runtimeName,
    props.model || "engine-selected model",
    maxTurns ? `up to ${maxTurns} steps` : "run-mode step limit",
    timeoutMinutes ? `${timeoutMinutes}-minute limit` : "run-mode time limit",
  ].join(" · ");

  return (
    <div className="space-y-5">
      <section
        aria-label="Configuration summary"
        className="rounded-xl border bg-muted/20 p-4"
      >
        <div className="mb-3">
          <h4 className="text-sm font-semibold">At a glance</h4>
          <p className="mt-1 text-xs text-muted-foreground">
            This is what the next run will use after the change is approved.
          </p>
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <SummaryCard
            title="Role"
            value={
              instructions.trim()
                ? "Follows the role and boundaries below"
                : "No role instructions have been written"
            }
          />
          <SummaryCard
            title="Reads before work"
            value={`${workspaceSummary}. ${toolsSummary}.`}
          />
          <SummaryCard title="Run setup" value={runSummary} />
        </div>
      </section>

      <ControlGroup
        eyebrow="What this agent does"
        description="Write the job, the expected response and the boundaries in ordinary language."
      >
        <div className="grid gap-4">
          <ControlField
            label="Role and instructions"
            htmlFor="agent-instructions"
            helper={`${instructions.length.toLocaleString("en-US")} characters · about ${tokenEstimate.toLocaleString("en-US")} tokens`}
          >
            {InstructionsEditor ? (
              <div
                role="region"
                aria-label="Instructions"
                aria-disabled={busy}
                className={`min-h-64 rounded-md border bg-background ${
                  busy ? "pointer-events-none opacity-60" : ""
                }`}
              >
                <InstructionsEditor
                  key={`${props.agent.id}:${editorKey}`}
                  defaultValue={instructions}
                  onUpdate={onInstructions}
                  placeholder="Write what this agent should do, how it should respond, and what it must never do…"
                  className="min-h-64 px-4 py-3 text-sm"
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
                rows={16}
                className="min-h-64 resize-y bg-background text-sm leading-relaxed"
              />
            )}
          </ControlField>
        </div>
      </ControlGroup>

      <ControlGroup
        eyebrow="What this agent reads before work"
        description="Choose how much shared guidance is added before each task. Tool access itself is managed under Tools."
      >
        <div className="grid gap-4 md:grid-cols-2">
          <ControlField
            label="Workspace guidance"
            htmlFor="ac-workspace-brief"
            helper="Shared rules, product context and working conventions."
          >
            <NativeSelect
              id="ac-workspace-brief"
              value={workspaceBriefMode}
              onChange={(event) => onWorkspaceBriefMode(event.target.value)}
              className="w-full"
            >
              <NativeSelectOption value="">
                Read all workspace guidance
              </NativeSelectOption>
              <NativeSelectOption value="off">
                Use this agent&apos;s role only
              </NativeSelectOption>
            </NativeSelect>
          </ControlField>

          <ControlField
            label="Tool descriptions"
            htmlFor="ac-tools-brief"
            helper="How much explanation the agent reads about tools it already has."
          >
            <NativeSelect
              id="ac-tools-brief"
              value={toolsBriefMode}
              onChange={(event) => onToolsBriefMode(event.target.value)}
              className="w-full"
            >
              <NativeSelectOption value="">
                Read full tool descriptions
              </NativeSelectOption>
              <NativeSelectOption value="summary">
                Read short connection summaries
              </NativeSelectOption>
            </NativeSelect>
          </ControlField>
        </div>
      </ControlGroup>

      <ControlGroup
        eyebrow="How this agent runs"
        description="Choose the engine, response effort and safety limits for one run."
      >
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-6">
          <ControlField
            label="Where it runs"
            htmlFor="ac-runtime"
            helper="Moving the agent is reviewed with the rest of this change."
          >
            <NativeSelect
              id="ac-runtime"
              aria-label="Where it runs"
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
            label="Response speed"
            htmlFor="ac-speed"
            helper="Fast is available only when the selected model supports it."
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
            label="Maximum steps"
            htmlFor="ac-max-turns"
            helper="Stops a run that keeps working without finishing."
            ignored={ignored("max_turns")}
          >
            <Input
              id="ac-max-turns"
              aria-label="Maximum steps"
              type="number"
              min={1}
              step={1}
              value={maxTurns}
              onChange={(event) => onMaxTurns(event.target.value)}
              placeholder="Use run-mode limit"
              disabled={ignored("max_turns")}
              className="font-mono text-xs"
            />
          </ControlField>
          <ControlField
            label="Time limit"
            htmlFor="ac-timeout-minutes"
            helper="Stops one run after this many minutes."
            ignored={ignored("timeout_minutes")}
          >
            <Input
              id="ac-timeout-minutes"
              aria-label="Time limit"
              type="number"
              min={1}
              step={1}
              value={timeoutMinutes}
              onChange={(event) => onTimeoutMinutes(event.target.value)}
              placeholder="Use run-mode limit"
              disabled={ignored("timeout_minutes")}
              className="font-mono text-xs"
            />
          </ControlField>
        </div>
      </ControlGroup>

      <details className="rounded-xl border bg-card">
        <summary className="cursor-pointer px-4 py-3 text-sm font-medium">
          Advanced
          <span className="ml-2 text-xs font-normal text-muted-foreground">
            instruction delivery and technical settings
          </span>
        </summary>
        <div className="space-y-4 border-t p-4">
          <ControlField
            label="Instruction delivery"
            htmlFor="ac-system-prompt-mode"
            helper="Controls how this role is combined with instructions supplied by the selected engine."
            ignored={ignored("system_prompt")}
          >
            <NativeSelect
              id="ac-system-prompt-mode"
              aria-label="Instruction delivery"
              value={systemPromptMode}
              onChange={(event) => onSystemPromptMode(event.target.value)}
              className="w-full md:max-w-xl"
              disabled={systemModes.length === 0}
            >
              <NativeSelectOption value="">
                Use the engine&apos;s default behavior
              </NativeSelectOption>
              {systemModes.includes("append") && (
                <NativeSelectOption value="append">
                  Keep engine instructions and add this role
                </NativeSelectOption>
              )}
              {systemModes.includes("replace") && (
                <NativeSelectOption value="replace">
                  Use only this agent&apos;s role
                </NativeSelectOption>
              )}
              {systemModes.includes("prepend") && (
                <NativeSelectOption value="prepend">
                  Send this role as task text
                </NativeSelectOption>
              )}
            </NativeSelect>
          </ControlField>

          <div className="rounded-lg border border-dashed px-4 py-3 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">
              Managed in their own tabs:
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
            </a>
            .
          </div>
        </div>
      </details>
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
      label={props.controlFirst ? "Reasoning effort" : "Thinking time"}
      htmlFor="ac-thinking"
      helper={
        props.controlFirst
          ? "Higher effort can improve difficult work, but takes longer."
          : "Values come from the selected model."
      }
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
  description,
  children,
}: {
  eyebrow: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card p-4">
      <div className="mb-4">
        <p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
          {eyebrow}
        </p>
        {description && (
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {children}
    </section>
  );
}

function SummaryCard({ title, value }: { title: string; value: string }) {
  return (
    <div className="rounded-lg border bg-background px-3 py-3">
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      <p className="mt-1 text-sm leading-relaxed">{value}</p>
    </div>
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

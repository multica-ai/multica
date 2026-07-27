"use client";

// Agent Office (FIR-1775) — Model / Thinking level selectors for the
// Propose-change modal. These were free-text inputs; this replaces them with
// the same catalog-driven pickers the rest of the app uses:
//   - Model: the runtime's daemon-discovered model catalog (falls back to a
//     free-text field when the runtime is offline or ignores model selection,
//     plus a "Custom…" escape hatch so any model id stays settable).
//   - Thinking level: the supported reasoning levels of the selected model
//     (empty = follow the local CLI config; hidden when the model has none).
//
// Self-contained on purpose: cerebro-agent-context must not import from
// @multica/views (that package imports this one via the tab extension, which
// would create a cycle), so we rebuild the selectors on the shared @multica/ui
// primitives + the same @multica/core catalog queries the upstream pickers use.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, RuntimeModel } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  runtimeListOptions,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";

const CUSTOM = "__custom__";

interface Props {
  agent: Agent;
  model: string;
  thinkingLevel: string;
  workspaceBriefMode: string;
  toolsBriefMode: string;
  onModel: (v: string) => void;
  onThinkingLevel: (v: string) => void;
  onWorkspaceBriefMode: (v: string) => void;
  onToolsBriefMode: (v: string) => void;
}

export function AgentContextConfigFields({
  agent,
  model,
  thinkingLevel,
  workspaceBriefMode,
  toolsBriefMode,
  onModel,
  onThinkingLevel,
  onWorkspaceBriefMode,
  onToolsBriefMode,
}: Props) {
  const wsId = useWorkspaceId();

  // Model discovery is only meaningful against an online runtime; mirror the
  // upstream pickers, which gate the query on the runtime being online.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const runtime = runtimes.find((r) => r.id === agent.runtime_id) ?? null;
  const runtimeOnline = runtime?.status === "online";

  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? agent.runtime_id : null),
  );
  const supported = modelsQuery.data?.supported ?? true;
  const models = useMemo(
    () => modelsQuery.data?.models ?? [],
    [modelsQuery.data],
  );

  // The catalog only drives the dropdown when we actually got models back;
  // otherwise fall back to the plain text field so an offline-runtime edit can
  // still set a model id manually (matches ModelDropdown's manual mode).
  const hasModelCatalog = supported && models.length > 0;
  const modelInList = models.some((m) => m.id === model);
  // Start in custom mode when a value is set that the catalog doesn't offer.
  const [modelCustom, setModelCustom] = useState(
    () => model !== "" && hasModelCatalog && !modelInList,
  );

  const thinkingEntry = pickModelEntry(models, model);
  const levels = thinkingEntry?.thinking?.supported_levels ?? [];
  const thinkingInList = levels.some((l) => l.value === thinkingLevel);

  return (
    <div className="space-y-3">
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {/* Model */}
      <div className="space-y-1.5">
        <Label htmlFor="ac-model" className="text-xs">
          Model
        </Label>
        {hasModelCatalog && !modelCustom ? (
          <NativeSelect
            id="ac-model"
            value={modelInList ? model : model === "" ? "" : CUSTOM}
            onChange={(e) => {
              const v = e.target.value;
              if (v === CUSTOM) {
                setModelCustom(true);
                return;
              }
              onModel(v);
            }}
            size="sm"
            className="w-full"
          >
            <NativeSelectOption value="">Default</NativeSelectOption>
            {models.map((m) => (
              <NativeSelectOption key={m.id} value={m.id}>
                {m.label}
              </NativeSelectOption>
            ))}
            {/* Keep an off-catalog value selectable rather than silently
                snapping it to Default. */}
            {model !== "" && !modelInList && (
              <NativeSelectOption value={CUSTOM}>
                {model} (custom)
              </NativeSelectOption>
            )}
            <NativeSelectOption value={CUSTOM}>Custom…</NativeSelectOption>
          </NativeSelect>
        ) : (
          <Input
            id="ac-model"
            value={model}
            onChange={(e) => onModel(e.target.value)}
            placeholder="default"
            className="h-8 font-mono text-xs"
          />
        )}
        {!hasModelCatalog && !modelsQuery.isLoading && (
          <p className="text-[10px] text-muted-foreground">
            {runtimeOnline
              ? "Runtime ignores per-agent model — leave blank."
              : "Runtime offline — enter model id manually."}
          </p>
        )}
      </div>

      {/* Thinking level */}
      <div className="space-y-1.5">
        <Label htmlFor="ac-thinking" className="text-xs">
          Thinking level
        </Label>
        {levels.length > 0 ? (
          <NativeSelect
            id="ac-thinking"
            value={thinkingLevel}
            onChange={(e) => onThinkingLevel(e.target.value)}
            size="sm"
            className="w-full"
          >
            <NativeSelectOption value="">Follow CLI config</NativeSelectOption>
            {levels.map((l) => (
              <NativeSelectOption key={l.value} value={l.value}>
                {l.label}
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
            value={thinkingLevel}
            onChange={(e) => onThinkingLevel(e.target.value)}
            placeholder="default"
            className="h-8 font-mono text-xs"
          />
        )}
      </div>

    </div>

      {/* FIR-3212: brief-layer modes. What the agent reads before its task —
          the shared workspace brief and the generated tools list. Both default
          to today's full brief; changing either flows through propose→approve
          like every other field. */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {/* Workspace brief */}
        <div className="space-y-1.5">
          <Label htmlFor="ac-workspace-brief" className="text-xs">
            Workspace brief
          </Label>
          <NativeSelect
            id="ac-workspace-brief"
            value={workspaceBriefMode}
            onChange={(e) => onWorkspaceBriefMode(e.target.value)}
            size="sm"
            className="w-full"
          >
            <NativeSelectOption value="">Full (default)</NativeSelectOption>
            <NativeSelectOption value="off">
              Off — only this agent&apos;s own role
            </NativeSelectOption>
          </NativeSelect>
          <p className="text-[10px] text-muted-foreground">
            Off strips the shared Multica brief so the agent reads only its own
            identity and role.
          </p>
        </div>

        {/* Tools brief */}
        <div className="space-y-1.5">
          <Label htmlFor="ac-tools-brief" className="text-xs">
            Tools list
          </Label>
          <NativeSelect
            id="ac-tools-brief"
            value={toolsBriefMode}
            onChange={(e) => onToolsBriefMode(e.target.value)}
            size="sm"
            className="w-full"
          >
            <NativeSelectOption value="">Full list (default)</NativeSelectOption>
            <NativeSelectOption value="summary">
              Summary — one line per connection
            </NativeSelectOption>
          </NativeSelect>
          <p className="text-[10px] text-muted-foreground">
            Summary folds each connection&apos;s generated tools to a single
            line to shrink the brief.
          </p>
        </div>
      </div>
    </div>
  );
}

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
): RuntimeModel | undefined {
  if (model) return models.find((m) => m.id === model);
  return models.find((m) => m.default) ?? models[0];
}

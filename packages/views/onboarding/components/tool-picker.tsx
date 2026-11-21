"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, RefreshCw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { ModelDropdown } from "../../agents/components/model-dropdown";
import { ThinkingPicker } from "../../agents/components/inspector/thinking-picker";
import { useThinkingLevels } from "../../agents/components/inspector/thinking-prop-row";
import { useT } from "../../i18n";
import { toolDisplayName } from "../provisioning";
import { RadioMark } from "./option-card";
import { InstallToolDialog, ToolTile } from "./install-tool-dialog";
import { INSTALLABLE_TOOLS, type InstallableTool } from "./tool-catalog";

export interface ToolSelection {
  runtimeId: string;
  /** Empty means the tool's own default. */
  model: string;
  /** Empty means the tool's own default. */
  thinkingLevel: string;
}

export const EMPTY_TOOL_SELECTION: ToolSelection = {
  runtimeId: "",
  model: "",
  thinkingLevel: "",
};

/**
 * Which coding tool becomes the agent. Tools on this computer are rows to
 * pick, with the model and thinking level inline under the picked one; the
 * popular tools that are not installed follow, each with how to install it.
 *
 * Selection is controlled. Until the member picks a row themselves the picker
 * keeps the top one selected, because the daemon registers its tools one by
 * one and the first to arrive is rarely the one to suggest. A manual pick is
 * never overridden.
 */
export function ToolPicker({
  runtimes,
  detecting,
  canRescan,
  onRescan,
  workspaceSlug,
  value,
  onChange,
  disabled = false,
}: {
  runtimes: AgentRuntime[];
  detecting: boolean;
  canRescan: boolean;
  onRescan: () => Promise<void>;
  workspaceSlug?: string;
  value: ToolSelection;
  onChange: (next: ToolSelection) => void;
  disabled?: boolean;
}) {
  const { t } = useT("onboarding");
  const [installing, setInstalling] = useState<InstallableTool | null>(null);
  const [rescanning, setRescanning] = useState(false);

  const installedProviders = new Set(runtimes.map((r) => r.provider));
  const missing = INSTALLABLE_TOOLS.filter((tool) => !installedProviders.has(tool.provider));
  // A selection already present on mount was made before Back left this
  // step, so it is kept like a manual pick.
  const pickedByMember = useRef(value.runtimeId !== "");
  const pick = (next: ToolSelection) => {
    pickedByMember.current = true;
    onChange(next);
  };

  useEffect(() => {
    const current = runtimes.some((r) => r.id === value.runtimeId);
    if (current && pickedByMember.current) return;
    const preferred = runtimes.find((r) => r.status === "online") ?? runtimes[0];
    if (preferred && preferred.id !== value.runtimeId) {
      onChange({ ...EMPTY_TOOL_SELECTION, runtimeId: preferred.id });
    }
  }, [runtimes, value.runtimeId, onChange]);

  // The tool being installed just registered: the dialog's job is done, and
  // the tool is what the member came back to pick.
  useEffect(() => {
    if (!installing) return;
    const arrived = runtimes.find((r) => r.provider === installing.provider);
    if (!arrived) return;
    setInstalling(null);
    pickedByMember.current = true;
    onChange({ ...EMPTY_TOOL_SELECTION, runtimeId: arrived.id });
  }, [installing, runtimes, onChange]);

  const rescan = async () => {
    setRescanning(true);
    try {
      await onRescan();
    } finally {
      setRescanning(false);
    }
  };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {runtimes.map((runtime) => (
        <RuntimeRow
          key={runtime.id}
          runtime={runtime}
          workspaceSlug={workspaceSlug}
          selected={runtime.id === value.runtimeId}
          value={value}
          onChange={pick}
          disabled={disabled}
        />
      ))}

      {detecting && runtimes.length === 0 ? (
        <div
          aria-busy
          className="flex items-center gap-3 rounded-lg border border-dashed px-4 py-3.5 text-body text-muted-foreground"
        >
          <Loader2 className="size-4 animate-spin" />
          {t(($) => $.tool_picker.detecting)}
        </div>
      ) : null}

      {!detecting
        ? missing.map((tool) => (
            <div
              key={tool.provider}
              className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3"
            >
              <ToolTile provider={tool.provider} />
              <div className="min-w-0 flex-1">
                <div className="truncate text-body font-medium text-foreground">
                  {toolDisplayName(tool.provider)}
                </div>
                <div className="truncate text-caption text-muted-foreground">
                  {t(($) => $.tool_picker.not_installed, { vendor: tool.vendor })}
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                disabled={disabled}
                onClick={() => setInstalling(tool)}
              >
                {t(($) => $.tool_picker.install)}
              </Button>
            </div>
          ))
        : null}

      <div className="flex items-center gap-3 pt-1">
        <p className="min-w-0 flex-1 text-caption text-muted-foreground">
          {t(($) => $.tool_picker.more_tools)}
        </p>
        {canRescan ? (
          <Button
            variant="ghost"
            size="sm"
            className="shrink-0 text-muted-foreground"
            onClick={rescan}
            disabled={rescanning || disabled}
            aria-busy={rescanning}
          >
            <RefreshCw className={cn(rescanning && "animate-spin")} />
            {t(($) => $.tool_picker.rescan)}
          </Button>
        ) : null}
      </div>

      <InstallToolDialog
        tool={installing}
        onOpenChange={(open) => {
          if (!open) setInstalling(null);
        }}
        canRescan={canRescan}
        onRescan={onRescan}
      />
    </div>
  );
}

function RuntimeRow({
  runtime,
  workspaceSlug,
  selected,
  value,
  onChange,
  disabled,
}: {
  runtime: AgentRuntime;
  workspaceSlug?: string;
  selected: boolean;
  value: ToolSelection;
  onChange: (next: ToolSelection) => void;
  disabled: boolean;
}) {
  const { t } = useT("onboarding");
  const online = runtime.status === "online";
  // device_info is "host · tool version"; metadata.cli_version is Multica's
  // own CLI, which says nothing about the tool.
  const meta = [runtime.device_info, online ? "" : t(($) => $.tool_picker.offline)]
    .filter(Boolean)
    .join(" · ");

  return (
    <div
      className={cn(
        "rounded-lg border bg-card transition-colors",
        selected ? "border-foreground/60 ring-1 ring-foreground/60" : "hover:border-foreground/20",
      )}
    >
      <button
        type="button"
        onClick={() => {
          if (!selected) onChange({ ...EMPTY_TOOL_SELECTION, runtimeId: runtime.id });
        }}
        disabled={disabled}
        aria-pressed={selected}
        className="flex w-full items-center gap-3 rounded-lg px-4 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none"
      >
        <RadioMark selected={selected} />
        <ToolTile provider={runtime.provider} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-body font-medium text-foreground">
            {toolDisplayName(runtime.provider)}
          </div>
          {meta ? (
            <div className="truncate text-caption text-muted-foreground">{meta}</div>
          ) : null}
        </div>
        <span
          aria-hidden
          className={cn(
            "size-2 shrink-0 rounded-full",
            online ? "bg-success" : "bg-muted-foreground/40",
          )}
        />
      </button>
      {selected ? (
        <RuntimeConfig
          runtime={runtime}
          workspaceSlug={workspaceSlug}
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
      ) : null}
    </div>
  );
}

function RuntimeConfig({
  runtime,
  workspaceSlug,
  value,
  onChange,
  disabled,
}: {
  runtime: AgentRuntime;
  workspaceSlug?: string;
  value: ToolSelection;
  onChange: (next: ToolSelection) => void;
  disabled: boolean;
}) {
  const { t } = useT("onboarding");
  const online = runtime.status === "online";
  const { levels, allowClear } = useThinkingLevels({
    runtimeId: runtime.id,
    runtimeOnline: online,
    provider: runtime.provider,
    model: value.model,
    workspaceSlug,
  });

  return (
    <div className="grid grid-cols-1 gap-3 border-t px-4 py-3 sm:grid-cols-2">
      <ModelDropdown
        runtimeId={runtime.id}
        workspaceSlug={workspaceSlug}
        runtimeOnline={online}
        value={value.model}
        onChange={(model) => {
          // Thinking levels hang off the model catalog entry, so a level
          // picked for the previous model may not exist on this one.
          if (model !== value.model) onChange({ ...value, model, thinkingLevel: "" });
        }}
        disabled={disabled}
      />
      {levels.length > 0 ? (
        <div className="flex min-w-0 flex-col gap-1.5">
          {/* Matches the label row ModelDropdown draws above its trigger. */}
          <span className="flex h-6 items-center text-caption text-muted-foreground">
            {t(($) => $.tool_picker.thinking)}
          </span>
          <ThinkingPicker
            variant="field"
            showLabel={false}
            value={value.thinkingLevel}
            levels={levels}
            allowClear={allowClear}
            canEdit={!disabled}
            onChange={(thinkingLevel) => onChange({ ...value, thinkingLevel })}
          />
        </div>
      ) : null}
    </div>
  );
}

"use client";

import type { AgentRuntime } from "@multica/core/types";
import { ModelDropdown } from "../../agents/components/model-dropdown";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { CompactRuntimeRow } from "./compact-runtime-row";

export interface AgentRuntimeSelection {
  runtimeId: string;
  /** Empty means "whatever the runtime defaults to". */
  model: string;
}

/**
 * The one place that asks "which runtime should this agent use, and on which
 * model" outside the agent form — the Runtimes page's "Create your agent".
 *
 * `layout` exists because the presentation genuinely differs, not because the
 * logic does: a list shows machines as they appear one at a time, a dropdown
 * keeps a dialog compact.
 */
export function AgentRuntimeChoice({
  runtimes,
  workspaceSlug,
  runtimesLoading,
  currentUserId = null,
  value,
  onChange,
  disabled = false,
  layout = "dropdown",
}: {
  runtimes: AgentRuntime[];
  workspaceSlug?: string;
  runtimesLoading?: boolean;
  /** Only used by the dropdown layout, to label runtime owners. */
  currentUserId?: string | null;
  value: AgentRuntimeSelection;
  onChange: (next: AgentRuntimeSelection) => void;
  disabled?: boolean;
  layout?: "dropdown" | "list";
}) {
  const selected = runtimes.find((rt) => rt.id === value.runtimeId) ?? null;

  const selectRuntime = (runtimeId: string) => {
    // Models are per-runtime, so a value chosen for the previous runtime may
    // not exist on this one. Owned here so no caller can forget it.
    onChange({
      runtimeId,
      model: runtimeId === value.runtimeId ? value.model : "",
    });
  };

  return (
    // min-w-0: DialogContent is a CSS grid, and a grid item's automatic
    // minimum width is its content width. Without this, an overlong runtime
    // or model name widens the grid column past the dialog and the pickers'
    // `truncate` never kicks in.
    <div className="flex min-w-0 flex-col gap-3">
      {layout === "list" ? (
        // Capped at ~4 rows so the install commands above stay reachable when
        // a member has many machines registered.
        <div className="flex max-h-[240px] flex-col gap-2 overflow-y-auto">
          {runtimes.map((rt) => (
            <CompactRuntimeRow
              key={rt.id}
              runtime={rt}
              selected={rt.id === value.runtimeId}
              onSelect={() => selectRuntime(rt.id)}
              disabled={disabled}
            />
          ))}
        </div>
      ) : (
        <RuntimePicker
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          members={[]}
          currentUserId={currentUserId}
          selectedRuntimeId={value.runtimeId}
          onSelect={selectRuntime}
          disabled={disabled}
        />
      )}
      <ModelDropdown
        workspaceSlug={workspaceSlug}
        runtimeId={value.runtimeId || null}
        runtimeOnline={selected?.status === "online"}
        value={value.model}
        onChange={(model) => onChange({ ...value, model })}
        disabled={!value.runtimeId || disabled}
      />
    </div>
  );
}

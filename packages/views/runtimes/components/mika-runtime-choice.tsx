"use client";

import type { AgentRuntime } from "@multica/core/types";
import { ModelDropdown } from "../../agents/components/model-dropdown";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { CompactRuntimeRow } from "./compact-runtime-row";

export interface MikaRuntimeSelection {
  runtimeId: string;
  /** Empty means "whatever the runtime defaults to". */
  model: string;
}

/**
 * The one place that asks "which runtime should Mika use, and on which model".
 *
 * Three surfaces reach this same decision — desktop onboarding, the web CLI
 * dialog, and the Runtimes page — and they had drifted: two offered a model
 * and one did not, so connecting via the web CLI silently created Mika on the
 * runtime's default model while desktop let you choose. Two of them also
 * re-implemented the reset rule below and the third simply lacked it.
 *
 * `layout` exists because the presentation genuinely differs, not because the
 * logic does. The CLI dialog lists machines because that is the moment they
 * appear one at a time after `multica setup`, and a collapsed dropdown hides
 * exactly the feedback that dialog is there to give.
 */
export function MikaRuntimeChoice({
  runtimes,
  runtimesLoading,
  currentUserId = null,
  value,
  onChange,
  disabled = false,
  layout = "dropdown",
}: {
  runtimes: AgentRuntime[];
  runtimesLoading?: boolean;
  /** Only used by the dropdown layout, to label runtime owners. */
  currentUserId?: string | null;
  value: MikaRuntimeSelection;
  onChange: (next: MikaRuntimeSelection) => void;
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
    <div className="flex flex-col gap-3">
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
        runtimeId={value.runtimeId || null}
        runtimeOnline={selected?.status === "online"}
        value={value.model}
        onChange={(model) => onChange({ ...value, model })}
        disabled={!value.runtimeId || disabled}
      />
    </div>
  );
}

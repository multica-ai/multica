"use client";

import type { RuntimeModelThinkingLevel } from "@multica/core/types";
// CEREBRO-PATCH(agent-runtime-setting-search-select): align Effort with the searchable Model picker.
import { RuntimeSettingSearchSelect } from "@multica/cerebro-ui/components/runtime-setting-search-select";
import { useT } from "../../../i18n";

/**
 * Per-agent reasoning/effort picker (MUL-2339). Renders only when the
 * current model exposes a non-empty `supported_levels` set — Claude, Codex,
 * and OpenCode today; every other provider gets nothing. The catalog is daemon-
 * discovered, so the value/label pairs match each CLI's own UI (`Low`,
 * `Extra high`, …) verbatim; never normalised across providers.
 *
 * Empty string is the "no override" sentinel: the backend omits the
 * effort flag entirely and the upstream CLI's own config / built-in
 * default decides what the model runs at. We render that state as
 * "Follow CLI config" rather than singling out one level as the
 * factory default, because the actual default at runtime is owned by
 * the user's local CLI install, not by Multica's catalog.
 */
export function ThinkingPicker({
  value,
  levels,
  canEdit = true,
  onChange,
}: {
  /** Persisted thinking_level — "" means "follow local CLI config". */
  value: string;
  /** Supported levels for the current (runtime, model) pair. Usually
   *  non-empty when the row is shown, but the stale-orphan clear path
   *  in ThinkingPropRow mounts the picker with an empty list plus a
   *  persisted value so the user can see and clear the dangling token. */
  levels: RuntimeModelThinkingLevel[];
  /** When false, render a static read-only display and skip the popover. */
  canEdit?: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const selected = value ? levels.find((l) => l.value === value) : undefined;
  // Unknown-but-set value (model swap that dropped the option, CLI upgrade
  // that trimmed the catalog): show the raw token so the user can see what
  // is actually persisted and clear it, rather than silently labelling it
  // "Default" when the backend would still send the stale value.
  const triggerLabel = selected
    ? selected.label
    : value || t(($) => $.pickers.thinking_default);
  const triggerTitle = t(($) => $.pickers.thinking_tooltip, { value: triggerLabel });

  if (!canEdit) {
    return (
      <span
        className="min-w-0 truncate px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
        title={triggerTitle}
      >
        {triggerLabel}
      </span>
    );
  }

  return (
    <RuntimeSettingSearchSelect
      variant="property"
      ariaLabel={t(($) => $.inspector.prop_thinking)}
      value={value}
      options={levels}
      defaultOption={{ value: "", label: t(($) => $.pickers.thinking_clear) }}
      defaultOptionTitle={t(($) => $.pickers.thinking_clear_title)}
      searchPlaceholder={t(($) => $.runtime_settings.search_effort)}
      onChange={onChange}
    />
  );
}

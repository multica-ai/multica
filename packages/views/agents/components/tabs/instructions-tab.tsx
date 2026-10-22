"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ReadonlyContent } from "../../../editor/readonly-content";
import { useT } from "../../../i18n";
import { SettingsViewTabs } from "../../../settings/components/settings-layout";

type EditorMode = "write" | "preview";

export function InstructionsTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (updates: { instructions: string }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [value, setValue] = useState(agent.instructions ?? "");
  const [mode, setMode] = useState<EditorMode>("write");
  const [saving, setSaving] = useState(false);
  const [systemOpen, setSystemOpen] = useState(false);
  const persisted = agent.instructions ?? "";
  const persistedRef = useRef({ agentId: agent.id, instructions: persisted });
  const localRef = useRef(value);
  localRef.current = value;
  const isDirty = value !== persisted;

  // A system agent's prompt has two halves: the product half ships with the
  // backend and updates on deploy, so it is shown read-only; the editable
  // field below holds only this workspace's own notes, which no release
  // overwrites. Ordinary agents have no system half and render unchanged.
  const systemInstructions = agent.system_instructions?.trim() ?? "";
  const hasSystemLayer = systemInstructions.length > 0;
  const fieldLabel = hasSystemLayer
    ? t(($) => $.tab_body.instructions.workspace_notes_label)
    : t(($) => $.tab_body.instructions.system_prompt_label);

  // Refetches can deliver the same instructions again or a newer server copy.
  // A server change is adopted only when the local editor was clean, so a
  // refetch never erases an edit; switching agents always loads the selection.
  useEffect(() => {
    const previous = persistedRef.current;
    const switchingAgents = previous.agentId !== agent.id;

    // The parent publishes the value optimistically while a save is in
    // flight. Neither that snapshot nor a later rollback is confirmed server
    // state, so keep the submitted value available for retry on failure.
    if (!switchingAgents && saving) return;

    const wasLocallyDirty = localRef.current !== previous.instructions;
    persistedRef.current = { agentId: agent.id, instructions: persisted };
    if (
      switchingAgents ||
      (!wasLocallyDirty && previous.instructions !== persisted)
    ) {
      setValue(persisted);
    }
  }, [agent.id, persisted, saving]);

  // Report dirty state up so the parent can guard tab switches.
  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({ instructions: value });
    } catch {
      // toast handled by parent
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-5">
      <p className="max-w-2xl text-pretty text-body leading-6 text-muted-foreground">
        {hasSystemLayer
          ? t(($) => $.tab_body.instructions.workspace_notes_intro)
          : t(($) => $.tab_body.instructions.intro)}
      </p>

      {hasSystemLayer && (
        <div className="rounded-lg border bg-muted/30">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5">
            <span className="text-body font-medium">
              {t(($) => $.tab_body.instructions.system_layer_label)}
            </span>
            <p className="min-w-0 flex-1 text-caption leading-snug text-muted-foreground">
              {t(($) => $.tab_body.instructions.system_layer_hint)}
            </p>
            <Button
              size="sm"
              variant="ghost"
              className="shrink-0"
              aria-expanded={systemOpen}
              onClick={() => setSystemOpen((open) => !open)}
            >
              {systemOpen
                ? t(($) => $.tab_body.instructions.system_layer_hide)
                : t(($) => $.tab_body.instructions.system_layer_show)}
            </Button>
          </div>
          {systemOpen && (
            <pre className="max-h-80 overflow-auto border-t px-3 py-2.5 text-caption leading-6 whitespace-pre-wrap text-muted-foreground">
              {systemInstructions}
            </pre>
          )}
        </div>
      )}

      {/* No overflow clipping on the card: the footer is sticky inside the
          page's scroll container, so Save stays reachable on a long prompt. */}
      <div className="rounded-xl border bg-card transition-[border-color,box-shadow] has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-3 has-[textarea:focus-visible]:ring-ring/50">
        <div className="flex h-11 items-center gap-3 border-b px-2">
          {hasSystemLayer ? (
            <span className="pl-1 text-label font-medium">{fieldLabel}</span>
          ) : null}
          <SettingsViewTabs<EditorMode>
            label={t(($) => $.tab_body.instructions.mode_aria)}
            items={[
              { value: "write", label: t(($) => $.tab_body.instructions.mode_write) },
              { value: "preview", label: t(($) => $.tab_body.instructions.mode_preview) },
            ]}
            value={mode}
            onChange={setMode}
          />
          <span className="ml-auto pr-1 text-caption tabular-nums text-muted-foreground">
            {t(($) => $.tab_body.instructions.char_count, {
              count: [...value].length,
            })}
          </span>
        </div>

        {mode === "write" ? (
          <Textarea
            id={`agent-system-prompt-${agent.id}`}
            name="agent-system-prompt"
            autoComplete="off"
            aria-label={fieldLabel}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            placeholder={
              hasSystemLayer
                ? t(($) => $.tab_body.instructions.workspace_notes_placeholder)
                : t(($) => $.tab_body.instructions.placeholder)
            }
            className="min-h-[28rem] resize-none rounded-none border-0 bg-transparent px-4 py-3 font-mono text-label leading-6 shadow-none focus-visible:ring-0 md:text-label dark:bg-transparent"
          />
        ) : (
          <div className="min-h-[28rem] px-4 py-3">
            {value.trim() ? (
              <ReadonlyContent content={value} />
            ) : (
              <p className="text-body text-muted-foreground">
                {t(($) => $.tab_body.instructions.preview_empty)}
              </p>
            )}
          </div>
        )}

        <div className="sticky bottom-0 flex min-h-12 items-center gap-2 rounded-b-xl border-t bg-card py-2 pr-2 pl-4">
          {isDirty ? (
            <span className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
              <span
                aria-hidden="true"
                className="size-1.5 shrink-0 rounded-full bg-warning"
              />
              {t(($) => $.tab_body.common.unsaved_changes)}
            </span>
          ) : null}
          <div className="ml-auto flex items-center gap-1.5">
            {isDirty ? (
              <Button
                size="sm"
                variant="ghost"
                disabled={saving}
                onClick={() => setValue(persisted)}
              >
                {t(($) => $.tab_body.common.discard)}
              </Button>
            ) : null}
            <Button size="sm" onClick={handleSave} disabled={!isDirty || saving}>
              {saving ? (
                <Loader2
                  className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                  aria-hidden="true"
                />
              ) : (
                <Save className="h-3.5 w-3.5" aria-hidden="true" />
              )}
              {t(($) => $.tab_body.common.save)}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

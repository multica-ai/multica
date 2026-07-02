"use client";

import { useEffect, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { MarkdownModeEditor } from "../../../editor";
import { useT } from "../../../i18n";

export function InstructionsTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (instructions: string) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [value, setValue] = useState(agent.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const isDirty = value !== (agent.instructions ?? "");

  // Sync when switching between agents.
  useEffect(() => {
    setValue(agent.instructions ?? "");
  }, [agent.id, agent.instructions]);

  // Report dirty state up so the parent can guard tab switches.
  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(value);
    } catch {
      // toast handled by parent
    } finally {
      setSaving(false);
    }
  };

  return (
    // Fill the parent TabContent (h-full flex-col): helper + footer take
    // their natural height, the editor wrapper fills the rest. Without this
    // the Save row scrolls off-screen as the user writes longer prompts.
    <div className="flex h-full flex-col gap-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.instructions.intro)}
      </p>

      <MarkdownModeEditor
        // Keyed by agent id so navigating between agents fully remounts the
        // rich editor — Tiptap's `defaultValue` is read once, so without the
        // key the second agent's instructions wouldn't load.
        key={agent.id}
        value={value}
        onChange={setValue}
        placeholder={t(($) => $.tab_body.instructions.placeholder)}
        labels={{
          rich: t(($) => $.tab_body.instructions.mode_rich),
          source: t(($) => $.tab_body.instructions.mode_source),
        }}
        debounceMs={150}
        // Mention has no business meaning in agent system prompts — typing
        // `@` would just confuse users with a member/agent picker.
        disableMentions
        className="flex-1 min-h-0"
        // flex-1 min-h-0 so the wrapper claims the leftover height in the
        // column. Rich mode scrolls inside the editor shell; source mode uses
        // the same shell and lets the textarea own the scroll.
        contentClassName="flex-1 min-h-0 overflow-hidden rounded-md border bg-background px-4 py-3 transition-colors focus-within:border-input"
        richEditorClassName="min-h-full"
        sourceEditorClassName="border-0 bg-transparent p-0 shadow-none focus-visible:ring-0"
      />

      <div className="flex items-center justify-end gap-3">
        {isDirty && (
          <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
        )}
        <Button
          size="sm"
          onClick={handleSave}
          disabled={!isDirty || saving}
        >
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

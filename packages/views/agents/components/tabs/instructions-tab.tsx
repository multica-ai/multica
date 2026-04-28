"use client";

import { useEffect, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { ContentEditor } from "../../../editor/content-editor";

const INSTRUCTIONS_PLACEHOLDER = `Define this agent's role, expertise, and working style.

# Example
You are a frontend engineer specializing in React and TypeScript.

## Working Style
- Write small, focused PRs — one commit per logical change
- Prefer composition over inheritance
- Always add unit tests for new components

## Constraints
- Do not modify shared/ types without explicit approval
- Follow the existing component patterns in features/`;

export function InstructionsTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (instructions: string) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
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
        Define this agent&apos;s identity and working style. Injected into the
        agent&apos;s context for every task. Markdown is supported.
      </p>

      <div
        // flex-1 min-h-0 so the wrapper claims the leftover height in the
        // column. overflow-y-auto so very long prompts scroll inside the
        // editor instead of pushing the Save row down.
        className="flex-1 min-h-0 overflow-y-auto rounded-md border bg-background px-4 py-3 transition-colors focus-within:border-input"
      >
        <ContentEditor
          // Keyed by agent id so navigating between agents fully remounts the
          // editor — Tiptap's `defaultValue` is read once, so without the key
          // the second agent's instructions wouldn't load.
          key={agent.id}
          defaultValue={value}
          onUpdate={setValue}
          placeholder={INSTRUCTIONS_PLACEHOLDER}
          debounceMs={150}
          // Mention has no business meaning in agent system prompts — typing
          // `@` would just confuse users with a member/agent picker.
          disableMentions
          // min-h-full lets the editor fill the wrapper even when the user
          // has typed nothing yet, so the click target matches the visual
          // box. Combined with the wrapper's overflow-y-auto, long content
          // grows past the wrapper height and scrolls within it.
          className="min-h-full"
        />
      </div>

      <div className="flex items-center justify-end gap-3">
        {isDirty && (
          <span className="text-xs text-muted-foreground">Unsaved changes</span>
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
          Save
        </Button>
      </div>
    </div>
  );
}

"use client";

import { useEffect, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent, AgentStarterPrompt } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../../i18n";

import { StarterPromptsEditor } from "../starter-prompts-editor";

export function InstructionsTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (updates: {
    instructions: string;
    starter_prompts: AgentStarterPrompt[];
  }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [value, setValue] = useState(agent.instructions ?? "");
  const [starterPrompts, setStarterPrompts] = useState<AgentStarterPrompt[]>(
    agent.starter_prompts ?? [],
  );
  const [saving, setSaving] = useState(false);
  const [systemOpen, setSystemOpen] = useState(false);
  const isDirty =
    value !== (agent.instructions ?? "") ||
    JSON.stringify(starterPrompts) !==
      JSON.stringify(agent.starter_prompts ?? []);
  const starterPromptsValid = starterPrompts.every(
    (item) => item.label.trim() && item.prompt.trim(),
  );

  // A system agent's prompt has two halves: the product half ships with the
  // backend and updates on deploy, so it is shown read-only; the editable
  // field below holds only this workspace's own notes, which no release
  // overwrites. Ordinary agents have no system half and render unchanged.
  const systemInstructions = agent.system_instructions?.trim() ?? "";
  const hasSystemLayer = systemInstructions.length > 0;

  // Sync when switching between agents.
  useEffect(() => {
    setValue(agent.instructions ?? "");
    setStarterPrompts(agent.starter_prompts ?? []);
  }, [agent.id, agent.instructions, agent.starter_prompts]);

  // Report dirty state up so the parent can guard tab switches.
  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({ instructions: value, starter_prompts: starterPrompts });
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

      <div className="space-y-2">
        <label
          htmlFor={`agent-system-prompt-${agent.id}`}
          className="text-body font-medium"
        >
          {hasSystemLayer
            ? t(($) => $.tab_body.instructions.workspace_notes_label)
            : t(($) => $.tab_body.instructions.system_prompt_label)}
        </label>
        <Textarea
          id={`agent-system-prompt-${agent.id}`}
          name="agent-system-prompt"
          autoComplete="off"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder={
            hasSystemLayer
              ? t(($) => $.tab_body.instructions.workspace_notes_placeholder)
              : t(($) => $.tab_body.instructions.placeholder)
          }
          rows={18}
          className="min-h-96 resize-y leading-6"
        />
      </div>

      <StarterPromptsEditor
        value={starterPrompts}
        onChange={setStarterPrompts}
      />

      <div className="flex items-center justify-end gap-3">
        {isDirty && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button
          size="sm"
          onClick={handleSave}
          disabled={!isDirty || !starterPromptsValid || saving}
        >
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
  );
}

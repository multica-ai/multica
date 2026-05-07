"use client";

import { useState, useEffect } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";

export function InstructionsTab({
  agent,
  readOnly = false,
  onSave,
}: {
  agent: Agent;
  readOnly?: boolean;
  onSave: (instructions: string) => Promise<void>;
}) {
  const [value, setValue] = useState(agent.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const isDirty = value !== (agent.instructions ?? "");

  // Sync when switching between agents.
  useEffect(() => {
    setValue(agent.instructions ?? "");
  }, [agent.id, agent.instructions]);

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
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">Agent Instructions</h3>
        <p className="text-xs text-muted-foreground mt-0.5">
          Define this agent&apos;s identity and working style. These instructions are
          injected into the agent&apos;s context for every task.
        </p>
        <p className="text-xs text-muted-foreground/70 mt-1 italic">
          This agent also inherits instructions from System defaults and your Personal defaults.
        </p>
      </div>

      <textarea
        value={value}
        onChange={(e) => { if (!readOnly) setValue(e.target.value); }}
        readOnly={readOnly}
        placeholder={readOnly ? "No instructions set" : `Define this agent's role, expertise, and working style.\n\nExample:\nYou are a frontend engineer specializing in React and TypeScript.\n\n## Working Style\n- Write small, focused PRs — one commit per logical change\n- Prefer composition over inheritance\n- Always add unit tests for new components\n\n## Constraints\n- Do not modify shared/ types without explicit approval\n- Follow the existing component patterns in features/`}
        className={`w-full min-h-[300px] rounded-md border bg-transparent px-3 py-2 text-sm font-mono placeholder:text-muted-foreground/50 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y ${readOnly ? "cursor-default bg-muted/50" : ""}`}
      />

      {!readOnly && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">
            {value.length > 0 ? `${value.length} characters` : "No instructions set"}
          </span>
          <Button
            size="xs"
            onClick={handleSave}
            disabled={!isDirty || saving}
          >
            {saving ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Save className="h-3 w-3" />
            )}
            Save
          </Button>
        </div>
      )}
    </div>
  );
}

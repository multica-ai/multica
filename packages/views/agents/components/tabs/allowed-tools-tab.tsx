"use client";

/* eslint-disable i18next/no-literal-string */

import { useEffect, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";

export function AllowedToolsTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (updates: Partial<Agent>) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const [text, setText] = useState((agent.allowed_tools ?? []).join("\n"));
  const [saving, setSaving] = useState(false);
  const current = text.split("\n").map((value) => value.trim()).filter(Boolean);
  const original = agent.allowed_tools ?? [];
  const dirty = JSON.stringify(current) !== JSON.stringify(original);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({ allowed_tools: current.length > 0 ? current : null });
      toast.success("Tool allowlist saved");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Unable to save tool allowlist");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">
          One provider-native tool name or pattern per line. Leave blank to allow the provider default.
        </p>
        <Textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          placeholder="mcp__builderlync__get_contact\nBash(multica issue *)"
          className="min-h-40 font-mono text-xs"
        />
      </div>
      <div className="flex items-center justify-end gap-3">
        {dirty && <span className="text-xs text-muted-foreground">Unsaved changes</span>}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save allowlist
        </Button>
      </div>
    </div>
  );
}

"use client";

import { useEffect, useState } from "react";
import {
  Eye,
  EyeOff,
  Loader2,
  Lock,
  Plus,
  Save,
  Trash2,
} from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";

let nextEnvId = 0;

interface EnvEntry {
  id: number;
  key: string;
  value: string;
  visible: boolean;
}

function envMapToEntries(env: Record<string, string>): EnvEntry[] {
  return Object.entries(env).map(([key, value]) => ({
    id: nextEnvId++,
    key,
    value,
    visible: false,
  }));
}

function entriesToEnvMap(entries: EnvEntry[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) {
      map[key] = entry.value;
    }
  }
  return map;
}

export function EnvTab({
  agent,
  readOnly = false,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  readOnly?: boolean;
  onSave: (updates: Partial<Agent>) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const [envEntries, setEnvEntries] = useState<EnvEntry[]>(
    envMapToEntries(agent.custom_env ?? {}),
  );
  const [saving, setSaving] = useState(false);

  const currentEnvMap = entriesToEnvMap(envEntries);
  const originalEnvMap = agent.custom_env ?? {};
  const dirty =
    JSON.stringify(currentEnvMap) !== JSON.stringify(originalEnvMap);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const addEnvEntry = () => {
    setEnvEntries([
      ...envEntries,
      { id: nextEnvId++, key: "", value: "", visible: true },
    ]);
  };

  const removeEnvEntry = (index: number) => {
    setEnvEntries(envEntries.filter((_, i) => i !== index));
  };

  const updateEnvEntry = (
    index: number,
    field: "key" | "value",
    val: string,
  ) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, [field]: val } : entry,
      ),
    );
  };

  const toggleEnvVisibility = (index: number) => {
    setEnvEntries(
      envEntries.map((entry, i) =>
        i === index ? { ...entry, visible: !entry.visible } : entry,
      ),
    );
  };

  const handleSave = async () => {
    const keys = envEntries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    const uniqueKeys = new Set(keys);
    if (uniqueKeys.size < keys.length) {
      toast.error("Duplicate environment variable keys");
      return;
    }

    setSaving(true);
    try {
      await onSave({ custom_env: currentEnvMap });
      toast.success("Environment variables saved");
    } catch {
      toast.error("Failed to save environment variables");
    } finally {
      setSaving(false);
    }
  };

  if (readOnly) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          Injected into the agent process at launch. Values are hidden — only
          the agent owner or workspace admin can view and edit them.
        </p>
        {envEntries.length > 0 ? (
          <div className="space-y-2">
            {envEntries.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2">
                <Input
                  value={entry.key}
                  readOnly
                  className="w-[40%] bg-muted font-mono text-xs"
                />
                <div className="relative flex-1">
                  <Input
                    type="password"
                    value="****"
                    readOnly
                    className="bg-muted pr-8 font-mono text-xs"
                  />
                  <Lock className="absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs italic text-muted-foreground">
            No environment variables configured.
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          Injected into the agent process at launch (e.g.{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            ANTHROPIC_API_KEY
          </code>
          ,{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            ANTHROPIC_BASE_URL
          </code>
          ).
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEnvEntry}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          Add
        </Button>
      </div>

      {envEntries.length > 0 && (
        <div className="space-y-2">
          {envEntries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.key}
                onChange={(e) => updateEnvEntry(index, "key", e.target.value)}
                placeholder="KEY"
                className="w-[40%] font-mono text-xs"
              />
              <div className="relative flex-1">
                <Input
                  type={entry.visible ? "text" : "password"}
                  value={entry.value}
                  onChange={(e) =>
                    updateEnvEntry(index, "value", e.target.value)
                  }
                  placeholder="value"
                  className="pr-8 font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={() => toggleEnvVisibility(index)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label={entry.visible ? "Hide value" : "Show value"}
                >
                  {entry.visible ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => removeEnvEntry(index)}
                className="text-muted-foreground hover:text-destructive"
                aria-label="Remove variable"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">Unsaved changes</span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
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

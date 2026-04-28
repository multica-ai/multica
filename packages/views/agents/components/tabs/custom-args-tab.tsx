"use client";

import { useState } from "react";
import {
  Loader2,
  Save,
  Plus,
  Trash2,
} from "lucide-react";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useAgentsT } from "../../i18n";

interface ArgEntry {
  id: string;
  value: string;
}

function argsToEntries(args: string[]): ArgEntry[] {
  return args.map((value) => ({ id: createSafeId(), value }));
}

function entriesToArgs(entries: ArgEntry[]): string[] {
  return entries.flatMap((e) => e.value.trim().split(/\s+/)).filter(Boolean);
}

export function CustomArgsTab({
  agent,
  runtimeDevice,
  onSave,
}: {
  agent: Agent;
  runtimeDevice?: RuntimeDevice;
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const t = useAgentsT();
  const [entries, setEntries] = useState<ArgEntry[]>(
    argsToEntries(agent.custom_args ?? []),
  );
  const [saving, setSaving] = useState(false);

  const currentArgs = entriesToArgs(entries);
  const originalArgs = agent.custom_args ?? [];
  const dirty = JSON.stringify(currentArgs) !== JSON.stringify(originalArgs);

  const addEntry = () => {
    setEntries([...entries, { id: createSafeId(), value: "" }]);
  };

  const removeEntry = (index: number) => {
    setEntries(entries.filter((_, i) => i !== index));
  };

  const updateEntry = (index: number, value: string) => {
    setEntries(
      entries.map((entry, i) => (i === index ? { ...entry, value } : entry)),
    );
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave({ custom_args: currentArgs });
      toast.success(t.customArgs.saved);
    } catch {
      toast.error(t.customArgs.saveFailed);
    } finally {
      setSaving(false);
    }
  };

  const launchHeader = runtimeDevice?.launch_header;

  return (
    <div className="max-w-lg space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label className="text-xs text-muted-foreground">
            {t.customArgs.title}
          </Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t.customArgs.description}
          </p>
          {launchHeader && (
            <p className="mt-2 text-xs text-muted-foreground">
              {t.customArgs.launchMode}{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
                {launchHeader} {t.customArgs.yourArgs}
              </code>
            </p>
          )}
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEntry}
          className="h-7 gap-1 text-xs"
        >
          <Plus className="h-3 w-3" />
          {t.customArgs.add}
        </Button>
      </div>
      {entries.length > 0 && (
        <div className="space-y-2">
          {entries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.value}
                onChange={(e) => updateEntry(index, e.target.value)}
                placeholder={t.customArgs.placeholder}
                className="flex-1 font-mono text-xs"
              />
              <button
                type="button"
                onClick={() => removeEntry(index)}
                className="shrink-0 text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}

      <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
        {saving ? (
          <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
        ) : (
          <Save className="h-3.5 w-3.5 mr-1.5" />
        )}
        {t.customArgs.save}
      </Button>
    </div>
  );
}

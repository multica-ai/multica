"use client";

import { useState } from "react";
import {
  Loader2,
  Save,
  Plus,
  Trash2,
} from "lucide-react";
import type { Agent } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";

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
  onSave,
}: {
  agent: Agent;
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
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
      toast.success("自定义参数已保存");
    } catch {
      toast.error("保存自定义参数失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-lg space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label className="text-xs text-muted-foreground">
            自定义参数
          </Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            启动时追加到智能体命令的额外 CLI 参数
            （例如 --model claude-sonnet-4-20250514）
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEntry}
          className="h-7 gap-1 text-xs"
        >
          <Plus className="h-3 w-3" />
          添加
        </Button>
      </div>
      {entries.length > 0 && (
        <div className="space-y-2">
          {entries.map((entry, index) => (
            <div key={entry.id} className="flex items-center gap-2">
              <Input
                value={entry.value}
                onChange={(e) => updateEntry(index, e.target.value)}
                placeholder="--model claude-sonnet-4-20250514"
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
        保存
      </Button>
    </div>
  );
}

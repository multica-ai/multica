"use client";

import { useState } from "react";
import {
  Loader2,
  Save,
  Plus,
  Trash2,
  Eye,
  EyeOff,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
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

const userEnvQueryKey = ["user-env"];

export function UserEnvTab() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: userEnvQueryKey,
    queryFn: () => api.getUserEnv(),
  });

  const serverEnv = data?.custom_env ?? {};
  const [envEntries, setEnvEntries] = useState<EnvEntry[] | null>(null);
  const [saving, setSaving] = useState(false);

  // Initialize from server on first load; track local edits separately
  const entries = envEntries ?? envMapToEntries(serverEnv);

  const currentEnvMap = entriesToEnvMap(entries);
  const dirty = JSON.stringify(currentEnvMap) !== JSON.stringify(serverEnv);

  const addEnvEntry = () => {
    setEnvEntries([
      ...entries,
      { id: nextEnvId++, key: "", value: "", visible: true },
    ]);
  };

  const removeEnvEntry = (index: number) => {
    setEnvEntries(entries.filter((_, i) => i !== index));
  };

  const updateEnvEntry = (index: number, field: "key" | "value", val: string) => {
    setEnvEntries(
      entries.map((entry, i) =>
        i === index ? { ...entry, [field]: val } : entry,
      ),
    );
  };

  const toggleEnvVisibility = (index: number) => {
    setEnvEntries(
      entries.map((entry, i) =>
        i === index ? { ...entry, visible: !entry.visible } : entry,
      ),
    );
  };

  const handleSave = async () => {
    const keys = entries.filter((e) => e.key.trim()).map((e) => e.key.trim());
    if (new Set(keys).size < keys.length) {
      toast.error("Duplicate environment variable keys");
      return;
    }

    setSaving(true);
    try {
      await api.updateUserEnv(currentEnvMap);
      qc.invalidateQueries({ queryKey: userEnvQueryKey });
      setEnvEntries(null);
      toast.success("Environment variables saved");
    } catch {
      toast.error("Failed to save environment variables");
    } finally {
      setSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading...
      </div>
    );
  }

  return (
    <div className="max-w-lg space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label className="text-xs text-muted-foreground">
            My Environment Variables
          </Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            Personal variables injected into tasks you create. Override workspace globals; can be overridden by agent-level vars.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEnvEntry}
          className="h-7 gap-1 text-xs"
        >
          <Plus className="h-3 w-3" />
          Add
        </Button>
      </div>

      {entries.length > 0 && (
        <div className="space-y-2">
          {entries.map((entry, index) => (
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
                  onChange={(e) => updateEnvEntry(index, "value", e.target.value)}
                  placeholder="value"
                  className="pr-8 font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={() => toggleEnvVisibility(index)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {entry.visible ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <button
                type="button"
                onClick={() => removeEnvEntry(index)}
                className="shrink-0 text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}

      {entries.length === 0 && (
        <p className="text-xs text-muted-foreground italic">No environment variables configured.</p>
      )}

      <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
        {saving ? (
          <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
        ) : (
          <Save className="h-3.5 w-3.5 mr-1.5" />
        )}
        Save
      </Button>
    </div>
  );
}

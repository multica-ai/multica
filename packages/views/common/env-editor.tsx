"use client";

import type { ReactNode } from "react";
import { Eye, EyeOff, Plus, Save, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

let nextEnvId = 0;

export interface EnvEditorEntry {
  id: number;
  key: string;
  value: string;
  visible: boolean;
}

export function envMapToEntries(env: Record<string, string>): EnvEditorEntry[] {
  return Object.entries(env).map(([key, value]) => ({
    id: nextEnvId++,
    key,
    value,
    visible: false,
  }));
}

export function entriesToEnvMap(entries: EnvEditorEntry[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) {
      map[key] = entry.value;
    }
  }
  return map;
}

export function EnvEditor({
  entries,
  onChange,
  onSave,
  saving,
  dirty,
  intro,
  addLabel,
  saveLabel,
  unsavedLabel,
  keyPlaceholder,
  valuePlaceholder,
  showValueLabel,
  hideValueLabel,
  removeLabel,
  emptyLabel,
}: {
  entries: EnvEditorEntry[];
  onChange: (entries: EnvEditorEntry[]) => void;
  onSave: () => void;
  saving: boolean;
  dirty: boolean;
  intro: ReactNode;
  addLabel: string;
  saveLabel: string;
  unsavedLabel: string;
  keyPlaceholder: string;
  valuePlaceholder: string;
  showValueLabel: string;
  hideValueLabel: string;
  removeLabel: string;
  emptyLabel: string;
}) {
  const updateEntry = (index: number, field: "key" | "value", value: string) => {
    onChange(entries.map((entry, i) => (i === index ? { ...entry, [field]: value } : entry)));
  };
  const toggleVisibility = (index: number) => {
    onChange(entries.map((entry, i) => (i === index ? { ...entry, visible: !entry.visible } : entry)));
  };
  const removeEntry = (index: number) => {
    onChange(entries.filter((_, i) => i !== index));
  };
  const addEntry = () => {
    onChange([...entries, { id: nextEnvId++, key: "", value: "", visible: true }]);
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">{intro}</p>
        <Button type="button" variant="outline" size="sm" onClick={addEntry} className="shrink-0">
          <Plus className="h-3 w-3" />
          {addLabel}
        </Button>
      </div>

      {entries.length > 0 ? (
        <div className="space-y-2">
          {entries.map((entry, index) => (
            <div key={entry.id} className="grid grid-cols-[minmax(0,0.4fr)_minmax(0,1fr)_2rem] items-center gap-2">
              <Input
                value={entry.key}
                onChange={(e) => updateEntry(index, "key", e.target.value)}
                placeholder={keyPlaceholder}
                className="min-w-0 font-mono text-xs"
              />
              <div className="relative min-w-0">
                <Input
                  type={entry.visible ? "text" : "password"}
                  value={entry.value}
                  onChange={(e) => updateEntry(index, "value", e.target.value)}
                  placeholder={valuePlaceholder}
                  className="pr-8 font-mono text-xs"
                />
                <button
                  type="button"
                  onClick={() => toggleVisibility(index)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label={entry.visible ? hideValueLabel : showValueLabel}
                >
                  {entry.visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => removeEntry(index)}
                className="text-muted-foreground hover:text-destructive"
                aria-label={removeLabel}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs italic text-muted-foreground">{emptyLabel}</p>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && <span className="text-xs text-muted-foreground">{unsavedLabel}</span>}
        <Button type="button" onClick={onSave} disabled={!dirty || saving} size="sm">
          <Save className="h-3.5 w-3.5" />
          {saveLabel}
        </Button>
      </div>
    </div>
  );
}

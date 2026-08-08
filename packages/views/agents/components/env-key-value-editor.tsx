"use client";

import { Eye, EyeOff, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * One editable row of the env editor. `id` is a client-side identity so React
 * keys survive reordering and blank keys; `visible` is per-row because a user
 * reveals one value at a time rather than unmasking the whole table.
 */
export interface EnvEntryDraft {
  id: number;
  key: string;
  value: string;
  visible: boolean;
}

let nextEnvEntryId = 0;

export function createEnvEntry(
  init: Partial<Omit<EnvEntryDraft, "id">> = {},
): EnvEntryDraft {
  return {
    id: nextEnvEntryId++,
    key: init.key ?? "",
    value: init.value ?? "",
    // A row the user just added holds a value they are typing, so masking it
    // by default would hide their own input from them.
    visible: init.visible ?? true,
  };
}

export function envEntriesToMap(
  entries: readonly EnvEntryDraft[],
): Record<string, string> {
  const map: Record<string, string> = {};
  for (const entry of entries) {
    const key = entry.key.trim();
    if (key) map[key] = entry.value;
  }
  return map;
}

/** First duplicated key, or null. Trailing whitespace is not a distinction:
 *  the server trims, so " KEY" and "KEY" would collide after the round-trip. */
export function findDuplicateEnvKey(
  entries: readonly EnvEntryDraft[],
): string | null {
  const seen = new Set<string>();
  for (const entry of entries) {
    const key = entry.key.trim();
    if (!key) continue;
    if (seen.has(key)) return key;
    seen.add(key);
  }
  return null;
}

/**
 * Key/value rows shared by the agent detail page's env tab (full management,
 * including deletion) and the quick injection dialog (add/overwrite only).
 * Both surfaces must mask values, offer the same per-row reveal toggle and
 * treat keys identically, so the markup lives in one place.
 *
 * Controlled: the parent owns the entry list. The editor never fetches or
 * persists anything.
 */
export function EnvKeyValueEditor({
  entries,
  onChange,
  disabled = false,
}: {
  entries: EnvEntryDraft[];
  onChange: (next: EnvEntryDraft[]) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");

  const update = (id: number, patch: Partial<EnvEntryDraft>) => {
    onChange(entries.map((e) => (e.id === id ? { ...e, ...patch } : e)));
  };

  return (
    <div className="space-y-2">
      {entries.map((entry) => (
        <div key={entry.id} className="flex items-center gap-2">
          <Input
            value={entry.key}
            disabled={disabled}
            onChange={(e) => update(entry.id, { key: e.target.value })}
            placeholder={t(($) => $.tab_body.env.key_placeholder)}
            className="w-[40%] font-mono text-caption"
          />
          <div className="relative flex-1">
            <Input
              type={entry.visible ? "text" : "password"}
              value={entry.value}
              disabled={disabled}
              onChange={(e) => update(entry.id, { value: e.target.value })}
              placeholder={t(($) => $.tab_body.env.value_placeholder)}
              className="pr-8 font-mono text-caption"
            />
            <button
              type="button"
              onClick={() => update(entry.id, { visible: !entry.visible })}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              aria-label={
                entry.visible
                  ? t(($) => $.tab_body.env.hide_value_aria)
                  : t(($) => $.tab_body.env.show_value_aria)
              }
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
            disabled={disabled}
            onClick={() => onChange(entries.filter((e) => e.id !== entry.id))}
            className="text-muted-foreground hover:text-destructive"
            aria-label={t(($) => $.tab_body.env.remove_aria)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      ))}
    </div>
  );
}

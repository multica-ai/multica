"use client";

import { useEffect, useState } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import { useT } from "../../../i18n";

interface ArgEntry {
  id: string;
  value: string;
}

function argsToEntries(args: string[]): ArgEntry[] {
  return args.map((value) => ({ id: createSafeId(), value }));
}

// Each row may contain a single arg ("--model") or several space-separated
// tokens ("--model claude-sonnet-4"). We split on whitespace so users can
// paste multi-token flags into one row without having to break them apart
// manually. The placeholder + helper text explain this so users aren't
// surprised when "--flag value" lands as two args at the back-end.
function entriesToArgs(entries: ArgEntry[]): string[] {
  return entries.flatMap((e) => e.value.trim().split(/\s+/)).filter(Boolean);
}

export function CustomArgsTab({
  agent,
  runtimeDevice,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtimeDevice?: RuntimeDevice;
  onSave: (updates: Partial<Agent>) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [entries, setEntries] = useState<ArgEntry[]>(
    argsToEntries(agent.custom_args ?? []),
  );
  const [saving, setSaving] = useState(false);

  const currentArgs = entriesToArgs(entries);
  const originalArgs = agent.custom_args ?? [];
  const dirty = JSON.stringify(currentArgs) !== JSON.stringify(originalArgs);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

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
      toast.success(t(($) => $.tab_body.custom_args.saved_toast));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.custom_args.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  const launchHeader = runtimeDevice?.launch_header;

  return (
    <div className="space-y-6">
      <p className="max-w-2xl text-pretty text-sm leading-6 text-muted-foreground">
        {t(($) => $.tab_body.custom_args.intro)}
      </p>

      {launchHeader && (
        <div className="border-y border-surface-border py-3 text-xs text-muted-foreground">
          <span>{t(($) => $.tab_body.custom_args.launch_mode_prefix)}</span>
          <code className="ml-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
            {launchHeader}{" "}
            {t(($) => $.tab_body.custom_args.launch_mode_args_placeholder)}
          </code>
        </div>
      )}

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-medium">
            {t(($) => $.tab_body.custom_args.arguments_label)}
          </h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={addEntry}
          >
            <Plus className="h-3 w-3" aria-hidden="true" />
            {t(($) => $.tab_body.common.add)}
          </Button>
        </div>

        {entries.length > 0 ? (
          <div className="divide-y divide-surface-border border-y border-surface-border">
            {entries.map((entry, index) => (
              <div key={entry.id} className="flex items-center gap-2 py-3">
                <Input
                  name={`agent-custom-arg-${index + 1}`}
                  autoComplete="off"
                  spellCheck={false}
                  value={entry.value}
                  onChange={(e) => updateEntry(index, e.target.value)}
                  placeholder={t(($) => $.tab_body.custom_args.input_placeholder)}
                  aria-label={t(($) => $.tab_body.custom_args.input_aria, {
                    index: index + 1,
                  })}
                  className="flex-1 font-mono text-xs"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => removeEntry(index)}
                  className="text-muted-foreground hover:text-destructive"
                  aria-label={t(($) => $.tab_body.custom_args.remove_aria)}
                >
                  <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <p className="border-y border-surface-border py-8 text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.custom_args.empty_title)}
          </p>
        )}
      </section>

      <div className="flex items-center justify-end gap-3 border-t border-surface-border pt-4">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
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

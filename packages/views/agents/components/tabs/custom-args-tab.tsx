"use client";

import { useEffect, useRef, useState } from "react";
import {
  Loader2,
  Pencil,
  Plus,
  Save,
  Terminal,
  Trash2,
} from "lucide-react";
import {
  CODEX_WINDOWS_SANDBOX_ARGS,
  ensureCodexWindowsSandboxArgs,
} from "@multica/core/agents";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import { useT } from "../../../i18n";
import {
  SettingsCard,
  SettingsSection,
} from "../../../settings/components/settings-layout";

interface ArgEntry {
  id: string;
  value: string;
}

type EditorState =
  | { kind: "add" }
  | { kind: "edit"; entryId: string }
  | null;

function argsToEntries(args: string[]): ArgEntry[] {
  return args.map((value) => ({ id: createSafeId(), value }));
}

function entriesToArgs(entries: ArgEntry[]): string[] {
  return entries.map((entry) => entry.value.trim()).filter(Boolean);
}

function formatArgForPreview(value: string): string {
  return /\s/.test(value) ? JSON.stringify(value) : value;
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
  const storedArgs = agent.custom_args ?? [];
  const storedManagedPrefix =
    agent.codex_windows_sandbox_arg_managed === true &&
    CODEX_WINDOWS_SANDBOX_ARGS.every(
      (value, index) => storedArgs[index] === value,
    );
  const originalArgs = storedManagedPrefix
    ? storedArgs.slice(CODEX_WINDOWS_SANDBOX_ARGS.length)
    : storedArgs;
  const [entries, setEntries] = useState<ArgEntry[]>(argsToEntries(originalArgs));
  const [editor, setEditor] = useState<EditorState>(null);
  const [editorValue, setEditorValue] = useState("");
  const [saving, setSaving] = useState(false);
  const editorInputRef = useRef<HTMLInputElement>(null);

  const editedArgs = entriesToArgs(entries);
  const currentArgs = ensureCodexWindowsSandboxArgs(editedArgs, runtimeDevice);
  const generatedPrefix =
    currentArgs.length ===
      editedArgs.length + CODEX_WINDOWS_SANDBOX_ARGS.length &&
    CODEX_WINDOWS_SANDBOX_ARGS.every(
      (value, index) => currentArgs[index] === value,
    )
      ? CODEX_WINDOWS_SANDBOX_ARGS
      : [];
  const dirty = JSON.stringify(editedArgs) !== JSON.stringify(originalArgs);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    if (editor) editorInputRef.current?.focus();
  }, [editor]);

  const startAdding = () => {
    setEditor({ kind: "add" });
    setEditorValue("");
  };

  const startEditing = (entry: ArgEntry) => {
    setEditor({ kind: "edit", entryId: entry.id });
    setEditorValue(entry.value);
  };

  const closeEditor = () => {
    setEditor(null);
    setEditorValue("");
  };

  const commitEditor = () => {
    const value = editorValue.trim();
    if (!editor || !value) return;

    if (editor.kind === "add") {
      setEntries((current) => [...current, { id: createSafeId(), value }]);
    } else {
      setEntries((current) =>
        current.map((entry) =>
          entry.id === editor.entryId ? { ...entry, value } : entry,
        ),
      );
    }
    closeEditor();
  };

  const removeEntry = (entryId: string) => {
    setEntries((current) => current.filter((entry) => entry.id !== entryId));
    if (editor?.kind === "edit" && editor.entryId === entryId) closeEditor();
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      // Send only editable/user-owned args. The server applies and records the
      // platform default atomically, which keeps provenance trustworthy and
      // prevents one managed token from being deleted independently.
      await onSave({
        custom_args: editedArgs,
        codex_windows_sandbox_arg_managed: false,
      });
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

  const renderEditor = (index?: number) => (
    <form
      className="rounded-lg border border-input bg-background p-2.5 shadow-xs"
      onSubmit={(event) => {
        event.preventDefault();
        commitEditor();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") closeEditor();
      }}
    >
      <Input
        ref={editorInputRef}
        name={editor?.kind === "add" ? "agent-custom-arg-new" : `agent-custom-arg-${index}`}
        autoComplete="off"
        spellCheck={false}
        value={editorValue}
        onChange={(event) => setEditorValue(event.target.value)}
        placeholder={t(($) => $.tab_body.custom_args.input_placeholder)}
        aria-label={
          editor?.kind === "add"
            ? t(($) => $.tab_body.custom_args.new_argument_aria)
            : t(($) => $.tab_body.custom_args.input_aria, { index })
        }
        className="font-mono text-caption"
      />
      <div className="mt-2 flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={closeEditor}>
          {t(($) => $.tab_body.custom_args.cancel_action)}
        </Button>
        <Button type="submit" size="sm" disabled={!editorValue.trim()}>
          {editor?.kind === "add"
            ? t(($) => $.tab_body.custom_args.add_action)
            : t(($) => $.tab_body.custom_args.update_action)}
        </Button>
      </div>
    </form>
  );

  const launchHeader = runtimeDevice?.launch_header;
  const launchCommand = launchHeader
    ? [launchHeader, ...currentArgs.map(formatArgForPreview)].join(" ")
    : null;

  return (
    <div className="space-y-6">
      <p className="max-w-2xl text-pretty text-body leading-6 text-muted-foreground">
        {t(($) => $.tab_body.custom_args.intro)}
      </p>

      <SettingsSection
        title={t(($) => $.tab_body.custom_args.arguments_label)}
        description={t(($) => $.tab_body.custom_args.arguments_description)}
        action={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={startAdding}
            disabled={editor !== null}
          >
            <Plus className="size-3.5" aria-hidden="true" />
            {t(($) => $.tab_body.custom_args.add_argument_action)}
          </Button>
        }
      >
        <SettingsCard>
          <div className="space-y-2 p-3">
            {entries.length === 0 &&
              generatedPrefix.length === 0 &&
              editor?.kind !== "add" ? (
              <div className="flex min-h-28 flex-col items-center justify-center px-4 py-6 text-center">
                <span className="flex size-9 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <Terminal className="size-4" aria-hidden="true" />
                </span>
                <p className="mt-3 text-body font-medium">
                  {t(($) => $.tab_body.custom_args.empty_title)}
                </p>
                <p className="mt-1 max-w-sm text-caption leading-5 text-muted-foreground">
                  {t(($) => $.tab_body.custom_args.empty_hint)}
                </p>
              </div>
            ) : null}

            <div role="list" className="space-y-2">
              {generatedPrefix.map((value, index) => (
                <div key={`managed-${value}`} role="listitem">
                  <div className="flex min-w-0 items-center gap-3 rounded-lg bg-muted/45 px-3 py-2.5">
                    <span className="w-5 shrink-0 text-center text-micro font-medium tabular-nums text-muted-foreground">
                      {index + 1}
                    </span>
                    <code
                      className="min-w-0 flex-1 break-all font-mono text-caption leading-5"
                      translate="no"
                    >
                      {value}
                    </code>
                  </div>
                </div>
              ))}
              {entries.map((entry, index) => (
                <div key={entry.id} role="listitem">
                  {editor?.kind === "edit" && editor.entryId === entry.id ? (
                    renderEditor(index + generatedPrefix.length + 1)
                  ) : (
                    <div className="group flex min-w-0 items-center gap-3 rounded-lg bg-muted/45 px-3 py-2.5 transition-colors hover:bg-muted/70">
                      <span className="w-5 shrink-0 text-center text-micro font-medium tabular-nums text-muted-foreground">
                        {index + generatedPrefix.length + 1}
                      </span>
                      <code
                        className="min-w-0 flex-1 break-all font-mono text-caption leading-5"
                        translate="no"
                      >
                        {entry.value}
                      </code>
                      <div className="flex shrink-0 items-center gap-0.5">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => startEditing(entry)}
                          disabled={editor !== null}
                          aria-label={t(($) => $.tab_body.custom_args.edit_aria, {
                            index: index + generatedPrefix.length + 1,
                          })}
                        >
                          <Pencil className="size-3.5" aria-hidden="true" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => removeEntry(entry.id)}
                          disabled={editor !== null}
                          className="text-muted-foreground hover:text-destructive"
                          aria-label={t(($) => $.tab_body.custom_args.remove_aria, {
                            index: index + generatedPrefix.length + 1,
                          })}
                        >
                          <Trash2 className="size-3.5" aria-hidden="true" />
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>

            {editor?.kind === "add" ? renderEditor() : null}
          </div>
        </SettingsCard>
      </SettingsSection>

      {launchCommand ? (
        <SettingsSection title={t(($) => $.tab_body.custom_args.command_preview_label)}>
          <SettingsCard>
            <div className="flex min-w-0 items-start gap-3 p-4">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                <Terminal className="size-3.5" aria-hidden="true" />
              </span>
              <code
                className="min-w-0 break-all pt-1.5 font-mono text-caption leading-5"
                translate="no"
              >
                {launchCommand}
              </code>
            </div>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      <div className="flex items-center justify-end gap-3 pt-1">
        {dirty ? (
          <span role="status" className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        ) : null}
        <Button
          onClick={handleSave}
          disabled={!dirty || saving || editor !== null}
          size="sm"
        >
          {saving ? (
            <Loader2
              className="size-3.5 animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            />
          ) : (
            <Save className="size-3.5" aria-hidden="true" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

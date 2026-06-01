"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Eraser, Loader2, Lock, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../../../i18n";

// `null` and the empty string are the two ways the user can mean "no
// config" — the server stores either as a NULL column and the daemon
// falls back to the runtime CLI default at launch. We normalise to
// the empty string in the editor so the dirty check has one canonical
// form to compare against.
function configToText(value: unknown): string {
  if (value === null || value === undefined) return "";
  return JSON.stringify(value, null, 2);
}

export function McpConfigTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (updates: { mcp_config: unknown | null }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");

  const redacted = agent.mcp_config_redacted === true;
  const original = useMemo(() => configToText(agent.mcp_config), [agent.mcp_config]);
  const [text, setText] = useState(original);
  const [saving, setSaving] = useState(false);

  // Sync local draft when the agent prop changes (e.g. after a successful
  // save invalidates the cache and a fresh agent arrives). We only sync
  // when the user has no in-flight edits — comparing the current draft
  // against the *previous* original (not the new one) is what tells us
  // "they haven't touched this since the last sync". Comparing against
  // the new original would skip the sync whenever the server-side value
  // changes underneath an untouched draft, leaving the editor showing a
  // stale value that a later Save would write back, clobbering another
  // admin's edit.
  const previousOriginalRef = useRef(original);
  useEffect(() => {
    setText((current) =>
      current === previousOriginalRef.current ? original : current,
    );
    previousOriginalRef.current = original;
  }, [original]);

  const trimmed = text.trim();
  const parseResult = useMemo<
    | { ok: true; value: unknown | null }
    | { ok: false; error: string }
  >(() => {
    if (trimmed === "") return { ok: true, value: null };
    try {
      const value = JSON.parse(trimmed);
      // The MCP CLI accepts an object (`{"mcpServers": …}`); a top-level
      // array or primitive is almost certainly a user mistake, so reject
      // here rather than surprise them with a server-side error later.
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        return {
          ok: false,
          error: "mcp_config_not_object",
        };
      }
      return { ok: true, value };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : "invalid JSON",
      };
    }
  }, [trimmed]);

  const dirty = text !== original;

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  if (redacted) {
    return (
      <div className="space-y-3">
        <p className="flex items-center gap-2 text-sm font-medium">
          <Lock className="h-3.5 w-3.5 text-muted-foreground" />
          {t(($) => $.tab_body.mcp_config.redacted_title)}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.mcp_config.redacted_hint)}
        </p>
      </div>
    );
  }

  const handleSave = async () => {
    if (!parseResult.ok) return;
    setSaving(true);
    try {
      await onSave({ mcp_config: parseResult.value });
      // Normalise the editor to the pretty-printed canonical form so the
      // dirty check stops firing after a successful save (the user's
      // raw input may differ from what configToText would emit).
      setText(configToText(parseResult.value));
      toast.success(t(($) => $.tab_body.mcp_config.saved_toast));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.mcp_config.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  const handleClear = () => {
    setText("");
  };

  const showInvalid = trimmed !== "" && !parseResult.ok;
  const invalidMessage = !parseResult.ok && parseResult.error === "mcp_config_not_object"
    ? t(($) => $.tab_body.mcp_config.invalid_not_object)
    : !parseResult.ok
      ? t(($) => $.tab_body.mcp_config.invalid_json, { error: parseResult.error })
      : "";

  return (
    <div className="flex h-full flex-col space-y-3">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.mcp_config.intro)}
        </p>
        {trimmed !== "" && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleClear}
            className="shrink-0"
          >
            <Eraser className="h-3 w-3" />
            {t(($) => $.tab_body.mcp_config.clear_action)}
          </Button>
        )}
      </div>

      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t(($) => $.tab_body.mcp_config.placeholder)}
        aria-invalid={showInvalid || undefined}
        aria-label={t(($) => $.tab_body.mcp_config.editor_aria)}
        spellCheck={false}
        className="min-h-[240px] flex-1 font-mono text-xs"
      />

      {showInvalid && (
        <p className="text-xs text-destructive">{invalidMessage}</p>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button
          onClick={handleSave}
          disabled={!dirty || !parseResult.ok || saving}
          size="sm"
        >
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

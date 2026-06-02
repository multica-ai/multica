"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Braces, Eraser, Loader2, Lock, Save, Shapes } from "lucide-react";
import type { Agent } from "@multica/core/types";
import type { McpServers } from "@multica/core/agents/mcp-validate";
import { validateMcpConfig } from "@multica/core/agents/mcp-validate";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../../../i18n";
import { McpServerEditor } from "../mcp-server-editor";

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Convert a config value to editor text (pretty-printed JSON). */
function configToText(value: unknown): string {
  if (value === null || value === undefined) return "";
  return JSON.stringify(value, null, 2);
}

/** Parse the mcpServers sub-object from the full config or null. */
function extractServers(value: unknown): McpServers | null {
  if (
    value === null ||
    value === undefined ||
    typeof value !== "object" ||
    Array.isArray(value)
  )
    return null;

  const root = value as Record<string, unknown>;
  const servers = root.mcpServers;
  if (
    typeof servers !== "object" ||
    servers === null ||
    Array.isArray(servers)
  )
    return null;

  return servers as McpServers;
}

/** Wrap servers into the full config shape: { mcpServers: { … } }. */
function wrapServers(servers: McpServers | null): unknown | null {
  if (!servers || Object.keys(servers).length === 0) return null;
  return { mcpServers: servers };
}

// ── Component ────────────────────────────────────────────────────────────────

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

  // "visual" or "json" — user can toggle.
  const [mode, setMode] = useState<"visual" | "json">("visual");

  // The parsed mcpServers for the visual editor.
  const servers = useMemo(() => extractServers(JSON.parse(text || "null")), [text]);

  // Sync local draft when the agent prop changes (e.g. after a successful
  // save invalidates the cache and a fresh agent arrives). We only sync
  // when the user has no in-flight edits — comparing the current draft
  // against the *previous* original (not the new one) is what tells us
  // "they haven't touched this since the last sync".
  const previousOriginalRef = useRef(original);
  useEffect(() => {
    setText((current) =>
      current === previousOriginalRef.current ? original : current,
    );
    previousOriginalRef.current = original;
  }, [original]);

  // ── Visual editor callbacks ──────────────────────────────────────────────

  const handleVisualChange = useCallback(
    (newServers: McpServers | null) => {
      const wrapped = wrapServers(newServers);
      setText(wrapped ? JSON.stringify(wrapped, null, 2) : "");
    },
    [],
  );

  // ── Raw editor callbacks ─────────────────────────────────────────────────

  const trimmed = text.trim();
  const parseResult = useMemo<
    | { ok: true; value: unknown | null }
    | { ok: false; error: string }
  >(() => {
    if (trimmed === "") return { ok: true, value: null };
    try {
      const value = JSON.parse(trimmed);
      // Client-side schema validation (mirrors server-side mcpvalidate).
      const schemaResult = validateMcpConfig(value);
      if (!schemaResult.ok) {
        return { ok: false, error: schemaResult.error };
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

  // ── Redacted view ────────────────────────────────────────────────────────

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

  // ── Save / Clear ─────────────────────────────────────────────────────────

  const handleSave = async () => {
    if (!parseResult.ok) return;
    setSaving(true);
    try {
      await onSave({ mcp_config: parseResult.value });
      // Normalise the editor to the pretty-printed canonical form so the
      // dirty check stops firing after a successful save.
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

  // ── Error messages ───────────────────────────────────────────────────────

  const showInvalid = trimmed !== "" && !parseResult.ok;
  const invalidMessage = !parseResult.ok
    ? parseResult.error.startsWith("mcp_config must") || parseResult.error.startsWith('"mcpServers"')
      ? t(($) => $.tab_body.mcp_config.invalid_not_object)
      : t(($) => $.tab_body.mcp_config.invalid_json, { error: parseResult.error })
    : "";

  // ── Render ───────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full flex-col space-y-3">
      {/* Intro + clear */}
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

      {/* Mode toggle */}
      <div className="flex items-center gap-1">
        <Button
          type="button"
          variant={mode === "visual" ? "secondary" : "ghost"}
          size="sm"
          onClick={() => setMode("visual")}
          className="h-7 gap-1 text-xs"
        >
          <Shapes className="h-3 w-3" />
          {t(($) => $.tab_body.mcp_config.mode_visual)}
        </Button>
        <Button
          type="button"
          variant={mode === "json" ? "secondary" : "ghost"}
          size="sm"
          onClick={() => setMode("json")}
          className="h-7 gap-1 text-xs"
        >
          <Braces className="h-3 w-3" />
          {t(($) => $.tab_body.mcp_config.mode_json)}
        </Button>
      </div>

      {/* Visual editor */}
      {mode === "visual" && (
        <div className="min-h-[200px] flex-1 overflow-auto">
          <McpServerEditor
            value={servers}
            onChange={handleVisualChange}
          />
        </div>
      )}

      {/* Raw JSON editor */}
      {mode === "json" && (
        <>
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
        </>
      )}

      {/* Save footer */}
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

"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, Lock, Save, Trash2 } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useT } from "../../../i18n";

// Stringify an mcp_config back to a pretty-printed JSON snippet for the
// textarea. Null / missing → empty string so the field renders empty.
function configToText(cfg: Agent["mcp_config"]): string {
  if (cfg == null) return "";
  return JSON.stringify(cfg, null, 2);
}

interface ValidationResult {
  ok: boolean;
  // When ok=false, an i18n key under tab_body.connectors.* plus a single
  // interpolation arg the caller substitutes. Kept as a structured pair so
  // the rendering layer chooses the locale.
  errorKey?:
    | "validation_invalid_json"
    | "validation_missing_mcpServers"
    | "validation_mcpServers_not_object"
    | "validation_invalid_server";
  errorArg?: string;
  parsed?: Record<string, unknown>;
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// Schema: { "mcpServers": { "<name>": { "command"?: string, "args"?: string[],
// "env"?: Record<string,string>, "url"?: string } } }
// Per the upstream Multica issue #2385 this matches Claude's standard MCP
// config shape. Validation is intentionally permissive (loose) on unknown
// top-level or per-server fields — Claude accepts more (transport, etc.)
// and the server is the source of truth; we just guard the high-confidence
// invariants so users get a fast inline error.
function validate(text: string): ValidationResult {
  const trimmed = text.trim();
  if (trimmed === "") {
    return { ok: true, parsed: undefined };
  }
  let raw: unknown;
  try {
    raw = JSON.parse(trimmed);
  } catch (e) {
    return {
      ok: false,
      errorKey: "validation_invalid_json",
      errorArg: e instanceof Error ? e.message : String(e),
    };
  }
  if (!isPlainObject(raw)) {
    return { ok: false, errorKey: "validation_missing_mcpServers" };
  }
  if (!("mcpServers" in raw)) {
    return { ok: false, errorKey: "validation_missing_mcpServers" };
  }
  const servers = raw.mcpServers;
  if (!isPlainObject(servers)) {
    return { ok: false, errorKey: "validation_mcpServers_not_object" };
  }
  for (const [name, spec] of Object.entries(servers)) {
    if (!isPlainObject(spec)) {
      return {
        ok: false,
        errorKey: "validation_invalid_server",
        errorArg: name,
      };
    }
    if ("command" in spec && typeof spec.command !== "string") {
      return {
        ok: false,
        errorKey: "validation_invalid_server",
        errorArg: name,
      };
    }
    if ("url" in spec && typeof spec.url !== "string") {
      return {
        ok: false,
        errorKey: "validation_invalid_server",
        errorArg: name,
      };
    }
    if (
      "args" in spec &&
      !(Array.isArray(spec.args) && spec.args.every((a) => typeof a === "string"))
    ) {
      return {
        ok: false,
        errorKey: "validation_invalid_server",
        errorArg: name,
      };
    }
    if (
      "env" in spec &&
      !(
        isPlainObject(spec.env) &&
        Object.values(spec.env).every((v) => typeof v === "string")
      )
    ) {
      return {
        ok: false,
        errorKey: "validation_invalid_server",
        errorArg: name,
      };
    }
  }
  return { ok: true, parsed: raw };
}

export function ConnectorsTab({
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
  const { t } = useT("agents");
  const originalText = useMemo(() => configToText(agent.mcp_config), [agent.mcp_config]);
  const [text, setText] = useState(originalText);
  const [saving, setSaving] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);

  const validation = validate(text);
  const dirty = text !== originalText;

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  // Re-seed the textarea when the underlying agent changes (e.g. after a
  // successful save the parent invalidates the query and re-renders us with
  // the new agent payload). Without this the textarea would keep showing the
  // pre-save text after the agent re-fetches.
  useEffect(() => {
    setText(originalText);
  }, [originalText]);

  const handleSave = async () => {
    if (!validation.ok) return;
    setSaving(true);
    try {
      // Empty textarea means "clear the config". The server treats explicit
      // `null` as a clear signal; sending `{}` would persist an empty object
      // and the daemon would still try to spawn with --mcp-config pointing
      // at it.
      const next = validation.parsed ?? null;
      await onSave({ mcp_config: next });
      toast.success(t(($) => $.tab_body.connectors.saved_toast));
    } catch {
      toast.error(t(($) => $.tab_body.connectors.save_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async () => {
    setConfirmClear(false);
    setSaving(true);
    try {
      await onSave({ mcp_config: null });
      setText("");
      toast.success(t(($) => $.tab_body.connectors.cleared_toast));
    } catch {
      toast.error(t(($) => $.tab_body.connectors.save_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  if (readOnly) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.connectors.intro_readonly)}
        </p>
        {agent.mcp_config_redacted ? (
          <div className="flex items-center gap-2 rounded-md border bg-muted px-3 py-2 text-xs">
            <Lock className="h-3.5 w-3.5 text-muted-foreground" />
            <span>{t(($) => $.tab_body.connectors.redacted_message)}</span>
          </div>
        ) : (
          <p className="text-xs italic text-muted-foreground">
            {t(($) => $.tab_body.connectors.empty_readonly)}
          </p>
        )}
      </div>
    );
  }

  const hasExistingConfig = agent.mcp_config != null;
  const errorMessage =
    !validation.ok && validation.errorKey
      ? t(($) => $.tab_body.connectors[validation.errorKey!], {
          error: validation.errorArg ?? "",
          name: validation.errorArg ?? "",
        })
      : null;

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.connectors.intro)}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.connectors.followup_prefix)}
          <a
            href="https://github.com/multica-ai/multica/issues/2385"
            target="_blank"
            rel="noreferrer"
            className="underline underline-offset-2 hover:text-foreground"
          >
            {t(($) => $.tab_body.connectors.followup_link_label)}
          </a>
          {t(($) => $.tab_body.connectors.followup_suffix)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">
            {"multica agent update --mcp-config-file"}
          </code>
          {"."}
        </p>
      </div>

      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t(($) => $.tab_body.connectors.json_placeholder)}
        spellCheck={false}
        className="min-h-[260px] font-mono text-xs"
        aria-label={t(($) => $.tab_body.connectors.editor_aria)}
        aria-invalid={!validation.ok}
      />

      {errorMessage && (
        <p className="text-xs text-destructive" role="alert">
          {errorMessage}
        </p>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && validation.ok && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        {hasExistingConfig && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setConfirmClear(true)}
            disabled={saving}
            className="text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t(($) => $.tab_body.connectors.clear_button)}
          </Button>
        )}
        <Button
          onClick={handleSave}
          disabled={!dirty || saving || !validation.ok}
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

      {confirmClear && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmClear(false);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.tab_body.connectors.clear_confirm_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tab_body.connectors.clear_confirm_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>
                {t(($) => $.tab_body.connectors.clear_confirm_cancel)}
              </AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={handleClear}>
                {t(($) => $.tab_body.connectors.clear_confirm_action)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </div>
  );
}

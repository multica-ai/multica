"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  Braces,
  Loader2,
  Lock,
  Save,
  Trash2,
} from "lucide-react";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { useT } from "../../../i18n";

type ParsedMcpConfig =
  | { ok: true; value: Record<string, unknown> | null }
  | { ok: false };

function formatMcpConfig(value: Record<string, unknown> | null | undefined) {
  return value ? JSON.stringify(value, null, 2) : "";
}

function sortJsonValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortJsonValue);
  }

  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, nestedValue]) => [key, sortJsonValue(nestedValue)]),
    );
  }

  return value;
}

function stringifyForComparison(value: Record<string, unknown> | null) {
  return JSON.stringify(sortJsonValue(value));
}

function parseMcpConfigInput(input: string): ParsedMcpConfig {
  const trimmed = input.trim();
  if (!trimmed) {
    return { ok: true, value: null };
  }

  try {
    const parsed = JSON.parse(trimmed) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { ok: false };
    }
    return { ok: true, value: parsed as Record<string, unknown> };
  } catch {
    return { ok: false };
  }
}

function isClaudeRuntime(runtimeDevice?: RuntimeDevice) {
  const provider = runtimeDevice?.provider.toLowerCase();
  return provider === "claude" || provider === "claude-code";
}

export function McpConfigTab({
  agent,
  runtimeDevice,
  readOnly = false,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtimeDevice?: RuntimeDevice;
  readOnly?: boolean;
  onSave: (updates: Partial<Agent>) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const [value, setValue] = useState(formatMcpConfig(agent.mcp_config));
  const [saving, setSaving] = useState(false);
  const runtimeSupportsMcp = isClaudeRuntime(runtimeDevice);

  useEffect(() => {
    setValue(formatMcpConfig(agent.mcp_config));
  }, [agent.id, agent.mcp_config]);

  const parsedInput = useMemo(() => parseMcpConfigInput(value), [value]);
  const originalConfig = agent.mcp_config ?? null;
  const dirty =
    parsedInput.ok
      ? stringifyForComparison(parsedInput.value) !==
        stringifyForComparison(originalConfig)
      : value !== formatMcpConfig(originalConfig);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleClear = () => {
    setValue("");
  };

  const handleSave = async () => {
    const parsed = parseMcpConfigInput(value);
    if (!parsed.ok) {
      toast.error(t(($) => $.tab_body.mcp.invalid_json_toast));
      return;
    }

    setSaving(true);
    try {
      await onSave({ mcp_config: parsed.value });
      toast.success(t(($) => $.tab_body.mcp.saved_toast));
    } catch {
      toast.error(t(($) => $.tab_body.mcp.save_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  if (readOnly) {
    return (
      <div className="space-y-4">
        <Alert>
          <Lock className="h-4 w-4" />
          <AlertTitle>{t(($) => $.tab_body.mcp.redacted_title)}</AlertTitle>
          <AlertDescription>
            {t(($) => $.tab_body.mcp.redacted_description)}
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <div className="space-y-3">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.mcp.intro)}
            </p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.mcp.empty_hint)}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleClear}
            disabled={!value.trim()}
            className="shrink-0"
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t(($) => $.tab_body.mcp.clear)}
          </Button>
        </div>

        {!runtimeSupportsMcp && (
          <Alert>
            <AlertTriangle className="h-4 w-4 text-warning" />
            <AlertTitle>{t(($) => $.tab_body.mcp.unsupported_title)}</AlertTitle>
            <AlertDescription>
              {runtimeDevice
                ? t(($) => $.tab_body.mcp.unsupported_description, {
                    provider: runtimeDevice.provider,
                  })
                : t(($) => $.tab_body.mcp.no_runtime_description)}
            </AlertDescription>
          </Alert>
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-2">
        <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
          <Braces className="h-3.5 w-3.5" />
          {t(($) => $.tab_body.mcp.editor_label)}
        </div>
        <Textarea
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder={t(($) => $.tab_body.mcp.placeholder)}
          aria-label={t(($) => $.tab_body.mcp.editor_aria)}
          aria-invalid={parsedInput.ok ? undefined : true}
          spellCheck={false}
          className="min-h-[320px] flex-1 resize-y field-sizing-fixed font-mono text-xs leading-5"
        />
      </div>

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
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

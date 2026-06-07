"use client";

import { useMemo, useState } from "react";
import { Loader2 } from "lucide-react";
import type { McpConnector } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";

/**
 * Substitutes `{{key}}` placeholders inside a connector's `mcp_template` with
 * the user-entered values, recursively. Only string leaves are touched; a
 * placeholder is replaced wherever it appears (a value can be a substring of a
 * larger string, e.g. `"Bearer {{TOKEN}}"`). Unknown placeholders are left
 * untouched rather than blanked, so a partially-filled template degrades
 * predictably instead of silently dropping config.
 */
export function substituteTemplate(
  template: unknown,
  values: Record<string, string>,
): unknown {
  if (typeof template === "string") {
    return template.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (match, key: string) => {
      const value = values[key];
      return value !== undefined ? value : match;
    });
  }
  if (Array.isArray(template)) {
    return template.map((item) => substituteTemplate(item, values));
  }
  if (template !== null && typeof template === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(template as Record<string, unknown>)) {
      out[k] = substituteTemplate(v, values);
    }
    return out;
  }
  return template;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Recursive deep merge: `next` wins on conflicts, but nested objects are
 * merged rather than replaced. Arrays and primitives are overwritten wholesale.
 * Used to fold a connector's resolved template into the agent's existing
 * `mcp_config` WITHOUT dropping any pre-existing `mcpServers` entries.
 */
export function deepMerge(base: unknown, next: unknown): unknown {
  if (isPlainObject(base) && isPlainObject(next)) {
    const out: Record<string, unknown> = { ...base };
    for (const [k, v] of Object.entries(next)) {
      out[k] = k in base ? deepMerge(base[k], v) : v;
    }
    return out;
  }
  return next;
}

/**
 * Folds a connector's resolved `mcp_template` into the agent's current
 * `mcp_config`. The agent config may be `null`/`undefined` (no config yet) or
 * a non-object (a corrupt value); both normalise to an empty object so the
 * merge always produces a valid `{ mcpServers: {...} }` shape and never
 * throws. Pre-existing servers are preserved.
 */
export function mergeConnectorIntoConfig(
  currentConfig: unknown,
  resolvedTemplate: unknown,
): Record<string, unknown> {
  const base = isPlainObject(currentConfig) ? currentConfig : {};
  const merged = deepMerge(base, isPlainObject(resolvedTemplate) ? resolvedTemplate : {});
  return merged as Record<string, unknown>;
}

/**
 * Schema-driven add-connector form. Renders one control per field in the
 * connector's `input_schema`, substitutes the entered values into the
 * connector's `mcp_template`, deep-merges the result into the agent's current
 * `mcp_config` (preserving every pre-existing `mcpServers` entry), and hands
 * the merged config to the existing `onSave({ mcp_config })` path — no new
 * agent endpoint. A connector with no fields can be added directly.
 */
export function ConnectorConfigForm({
  connector,
  currentConfig,
  open,
  onOpenChange,
  onSave,
}: {
  connector: McpConnector;
  /** The agent's current `mcp_config` value (possibly null). */
  currentConfig: unknown;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The existing agent-save path: persists the merged `mcp_config`. */
  onSave: (updates: { mcp_config: unknown }) => Promise<void>;
}) {
  const fields = useMemo(
    () => connector.input_schema?.fields ?? [],
    [connector.input_schema],
  );
  const [values, setValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  const missingRequired = fields.some(
    (f) => f.required === true && !(values[f.key] ?? "").trim(),
  );

  const handleSubmit = async () => {
    if (missingRequired || saving) return;
    setSaving(true);
    try {
      const resolved = substituteTemplate(connector.mcp_template, values);
      const merged = mergeConnectorIntoConfig(currentConfig, resolved);
      await onSave({ mcp_config: merged });
      onOpenChange(false);
      setValues({});
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : "Failed to add connector",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-full max-w-md">
        <DialogHeader>
          <DialogTitle className="truncate">Add {connector.name}</DialogTitle>
          <DialogDescription>
            {connector.description
              ? connector.description
              : "Add this connector to the agent's MCP configuration."}
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void handleSubmit();
          }}
        >
          {fields.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              This connector needs no configuration and can be added directly.
            </p>
          ) : (
            fields.map((field) => {
              const inputId = `mcp-field-${field.key}`;
              // Unknown control types fall back to a text input — enum drift
              // downgrades rather than crashes.
              const inputType =
                field.type === "password"
                  ? "password"
                  : field.type === "number"
                    ? "number"
                    : "text";
              return (
                <div key={field.key} className="space-y-1.5">
                  <Label htmlFor={inputId}>
                    {field.label || field.key}
                    {field.required === true && (
                      <span className="ml-0.5 text-destructive">*</span>
                    )}
                  </Label>
                  <Input
                    id={inputId}
                    type={inputType}
                    value={values[field.key] ?? ""}
                    placeholder={field.placeholder ?? ""}
                    onChange={(e) =>
                      setValues((prev) => ({
                        ...prev,
                        [field.key]: e.target.value,
                      }))
                    }
                  />
                  {field.help && (
                    <p className="text-xs text-muted-foreground">{field.help}</p>
                  )}
                </div>
              );
            })
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={missingRequired || saving}
            >
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : null}
              Add connector
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

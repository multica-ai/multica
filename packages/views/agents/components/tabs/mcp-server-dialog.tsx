"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, Plus, Trash2 } from "lucide-react";
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
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../../i18n";
import type { ManagedMcpServer } from "./mcp-config-model";
import { isRecord, mcpTransport } from "./mcp-config-model";

type EditorMode = "form" | "json";
type FormTransport = "stdio" | "http";
type KeyValue = { key: string; value: string };

type McpFormState = {
  transport: FormTransport;
  command: string;
  args: string[];
  env: KeyValue[];
  url: string;
  headers: KeyValue[];
  extras: Record<string, unknown>;
};

const emptyForm = (): McpFormState => ({
  transport: "stdio",
  command: "",
  args: [],
  env: [],
  url: "",
  headers: [],
  extras: {},
});

function pairsFromRecord(value: unknown): KeyValue[] {
  if (!isRecord(value)) return [];
  return Object.entries(value).flatMap(([key, item]) =>
    typeof item === "string" ? [{ key, value: item }] : [],
  );
}

function formFromConfig(config: Record<string, unknown>): McpFormState {
  const extras = { ...config };
  for (const key of [
    "type",
    "command",
    "args",
    "env",
    "environment",
    "url",
    "headers",
  ]) {
    delete extras[key];
  }

  const transport = mcpTransport(config) === "stdio" ? "stdio" : "http";
  let command = "";
  let args: string[] = [];
  if (typeof config.command === "string") command = config.command;
  else if (Array.isArray(config.command)) {
    const tokens = config.command.filter(
      (value): value is string => typeof value === "string",
    );
    command = tokens[0] ?? "";
    args = tokens.slice(1);
  }
  if (Array.isArray(config.args)) {
    args = config.args.filter(
      (value): value is string => typeof value === "string",
    );
  }

  return {
    transport,
    command,
    args,
    env: pairsFromRecord(config.env ?? config.environment),
    url: typeof config.url === "string" ? config.url : "",
    headers: pairsFromRecord(config.headers),
    extras,
  };
}

function recordFromPairs(pairs: KeyValue[]): Record<string, string> | undefined {
  const entries = pairs
    .map(({ key, value }) => [key.trim(), value] as const)
    .filter(([key]) => key !== "");
  return entries.length > 0 ? Object.fromEntries(entries) : undefined;
}

/**
 * The guided form can only express two transports, and saving from it REWRITES
 * the entry (`configFromForm` emits `type: "http"` for anything non-stdio). An
 * entry on any other transport — `sse` today, whatever a newer backend reports
 * tomorrow — must therefore never be routed through the form, or editing it
 * would silently change its protocol and can break the server outright.
 * Those entries take the JSON path, which round-trips whatever is typed.
 *
 * This one classifies a NON-secret summary transport, which the server emits
 * from a closed set (stdio / http / sse / unknown).
 */
function formCanExpressTransport(transport: string): boolean {
  return transport === "stdio" || transport === "http";
}

/** The `type` values `configFromForm` can write back without changing them. */
const FORM_EXPRESSIBLE_TYPES = new Set([
  "local",
  "stdio",
  "remote",
  "http",
  "streamable-http",
]);

/**
 * Whether the form can round-trip a SAVED entry.
 *
 * Deliberately does not reuse `mcpTransport`: that is a lossy DISPLAY
 * classifier — it reports "http" for any entry carrying a `url`, whatever its
 * `type` — so `{"type":"websocket","url":"wss://…"}` would look form-editable
 * and be rewritten to `type: "http"` on save. Safety decisions need the
 * explicit value, not the display bucket.
 */
function formCanExpressConfig(config: Record<string, unknown>): boolean {
  const type =
    typeof config.type === "string" ? config.type.trim().toLowerCase() : "";
  if (type !== "") return FORM_EXPRESSIBLE_TYPES.has(type);
  // No explicit type: the form infers stdio from `command` and http from
  // `url`, and writes that same shape back. With neither there is nothing for
  // it to express, so leave the entry to the JSON editor.
  return config.command !== undefined || config.url !== undefined;
}

function configFromForm(form: McpFormState): Record<string, unknown> {
  const config = { ...form.extras };
  if (form.transport === "stdio") {
    config.command = form.command.trim();
    if (form.args.length > 0) config.args = form.args;
    const env = recordFromPairs(form.env);
    if (env) config.env = env;
  } else {
    config.type = "http";
    config.url = form.url.trim();
    const headers = recordFromPairs(form.headers);
    if (headers) config.headers = headers;
  }
  return config;
}

function parseServerJson(text: string):
  | { ok: true; value: Record<string, unknown> }
  | { ok: false; error: string } {
  try {
    const value = JSON.parse(text);
    if (!isRecord(value)) return { ok: false, error: "not_object" };
    if (!value.command && !value.url) return { ok: false, error: "missing_target" };
    return { ok: true, value };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "invalid JSON",
    };
  }
}

/**
 * Whether the guided form can edit this entry without changing it. The legacy
 * `mcp` container is provider-native and must round-trip verbatim; so is any
 * entry whose transport the form cannot express. When the caller could not read
 * the saved config back, the safe summary's transport is the only signal.
 */
function formSupportsServer(server: ManagedMcpServer): boolean {
  if (server.container === "mcp") return false;
  // With a saved entry in hand, judge the entry itself; the summary transport
  // is only a fallback for callers that could not read one back.
  return Object.keys(server.config).length > 0
    ? formCanExpressConfig(server.config)
    : formCanExpressTransport(server.transport);
}

export function McpServerDialog({
  open,
  server,
  existingNames,
  lockName = false,
  onOpenChange,
  onSave,
}: {
  open: boolean;
  server: ManagedMcpServer | null;
  existingNames: Set<string>;
  /**
   * Pins the name while editing. Callers whose backend has no atomic rename
   * must set this: letting the field change would turn "save" into "create a
   * second server" and silently leave the original behind.
   */
  lockName?: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, config: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const [name, setName] = useState("");
  const [mode, setMode] = useState<EditorMode>("form");
  const [form, setForm] = useState<McpFormState>(emptyForm);
  const [jsonText, setJsonText] = useState("{}");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    const config = server?.config ?? {};
    setName(server?.name ?? "");
    // A caller that cannot read the saved entry back (the workspace layer is
    // write-only) passes an empty config with the transport from the safe
    // summary. Seeding from `formFromConfig({})` there would open an http form
    // for a known stdio server, so take the transport from the summary.
    setForm(
      !server
        ? emptyForm()
        : Object.keys(config).length > 0
          ? formFromConfig(config)
          : { ...emptyForm(), transport: server.transport === "stdio" ? "stdio" : "http" },
    );
    setJsonText(JSON.stringify(config, null, 2));
    setMode(server && !formSupportsServer(server) ? "json" : "form");
  }, [open, server]);

  // The form is unavailable — not merely unselected — for entries it cannot
  // represent, so switching to it cannot rewrite them either.
  const formAvailable = !server || formSupportsServer(server);

  const jsonResult = useMemo(() => parseServerJson(jsonText), [jsonText]);
  const trimmedName = name.trim();
  const nameError =
    trimmedName === ""
      ? "required"
      : !/^[A-Za-z0-9_-]+$/.test(trimmedName)
        ? "format"
        : existingNames.has(trimmedName) && trimmedName !== server?.name
          ? "duplicate"
          : null;
  const formError =
    form.transport === "stdio"
      ? form.command.trim() === ""
        ? "command"
        : null
      : form.url.trim() === ""
        ? "url"
        : null;
  const canSave =
    !saving &&
    nameError === null &&
    (mode === "form" ? formError === null : jsonResult.ok);

  const errorMessage =
    nameError === "required"
      ? t(($) => $.tab_body.mcp_config.dialog_name_required)
      : nameError === "format"
        ? t(($) => $.tab_body.mcp_config.dialog_name_invalid)
        : nameError === "duplicate"
          ? t(($) => $.tab_body.mcp_config.dialog_name_duplicate)
          : mode === "form" && formError === "command"
            ? t(($) => $.tab_body.mcp_config.dialog_command_required)
            : mode === "form" && formError === "url"
              ? t(($) => $.tab_body.mcp_config.dialog_url_required)
              : mode === "json" && !jsonResult.ok && jsonResult.error === "not_object"
                ? t(($) => $.tab_body.mcp_config.dialog_json_object)
                : mode === "json" && !jsonResult.ok && jsonResult.error === "missing_target"
                  ? t(($) => $.tab_body.mcp_config.dialog_json_target)
                  : mode === "json" && !jsonResult.ok
                    ? t(($) => $.tab_body.mcp_config.invalid_json, {
                        error: jsonResult.error,
                      })
                    : "";

  const handleModeChange = (next: string | number | null) => {
    if (next !== "form" && next !== "json") return;
    if (next === "json" && mode === "form") {
      setJsonText(JSON.stringify(configFromForm(form), null, 2));
    } else if (next === "form" && mode === "json" && jsonResult.ok) {
      setForm(formFromConfig(jsonResult.value));
    }
    setMode(next);
  };

  const handleSave = async () => {
    if (!canSave) return;
    let config: Record<string, unknown>;
    if (mode === "form") config = configFromForm(form);
    else if (jsonResult.ok) config = jsonResult.value;
    else return;
    setSaving(true);
    try {
      await onSave(trimmedName, config);
      onOpenChange(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {server
              ? t(($) => $.tab_body.mcp_config.dialog_edit_title)
              : t(($) => $.tab_body.mcp_config.dialog_add_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.tab_body.mcp_config.dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="mcp-server-name">
            {t(($) => $.tab_body.mcp_config.dialog_name_label)}
          </Label>
          <Input
            id="mcp-server-name"
            name="mcp-server-name"
            autoComplete="off"
            spellCheck={false}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t(($) => $.tab_body.mcp_config.dialog_name_placeholder)}
            aria-invalid={nameError !== null || undefined}
            readOnly={lockName}
            aria-readonly={lockName || undefined}
            className={lockName ? "text-muted-foreground" : undefined}
          />
          {lockName ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.mcp_config.dialog_name_locked)}
            </p>
          ) : null}
        </div>

        <Tabs value={mode} onValueChange={handleModeChange}>
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="form" disabled={!formAvailable}>
              {t(($) => $.tab_body.mcp_config.dialog_form_tab)}
            </TabsTrigger>
            <TabsTrigger value="json">
              {t(($) => $.tab_body.mcp_config.dialog_json_tab)}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="form" className="space-y-5 pt-2">
            <fieldset className="space-y-2">
              <legend className="text-body font-medium">
                {t(($) => $.tab_body.mcp_config.dialog_type_label)}
              </legend>
              <div className="grid grid-cols-2 gap-2">
                {(["stdio", "http"] as const).map((transport) => (
                  <Button
                    key={transport}
                    type="button"
                    variant={form.transport === transport ? "secondary" : "outline"}
                    aria-pressed={form.transport === transport}
                    onClick={() => setForm((current) => ({ ...current, transport }))}
                  >
                    {transport === "stdio"
                      ? t(($) => $.tab_body.mcp_config.dialog_type_stdio)
                      : t(($) => $.tab_body.mcp_config.dialog_type_http)}
                  </Button>
                ))}
              </div>
            </fieldset>

            {form.transport === "stdio" ? (
              <>
                <div className="space-y-2">
                  <Label htmlFor="mcp-server-command">
                    {t(($) => $.tab_body.mcp_config.dialog_command_label)}
                  </Label>
                  <Input
                    id="mcp-server-command"
                    name="mcp-server-command"
                    autoComplete="off"
                    spellCheck={false}
                    value={form.command}
                    onChange={(event) =>
                      setForm((current) => ({ ...current, command: event.target.value }))
                    }
                    placeholder="npx"
                    aria-invalid={formError === "command" || undefined}
                  />
                </div>
                <StringListEditor
                  label={t(($) => $.tab_body.mcp_config.dialog_args_label)}
                  addLabel={t(($) => $.tab_body.mcp_config.dialog_add_arg)}
                  removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_arg)}
                  values={form.args}
                  onChange={(args) => setForm((current) => ({ ...current, args }))}
                />
                <KeyValueEditor
                  label={t(($) => $.tab_body.mcp_config.dialog_env_label)}
                  addLabel={t(($) => $.tab_body.mcp_config.dialog_add_env)}
                  removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_env)}
                  rows={form.env}
                  onChange={(env) => setForm((current) => ({ ...current, env }))}
                />
              </>
            ) : (
              <>
                <div className="space-y-2">
                  <Label htmlFor="mcp-server-url">
                    {t(($) => $.tab_body.mcp_config.dialog_url_label)}
                  </Label>
                  <Input
                    id="mcp-server-url"
                    name="mcp-server-url"
                    type="url"
                    inputMode="url"
                    autoComplete="off"
                    spellCheck={false}
                    value={form.url}
                    onChange={(event) =>
                      setForm((current) => ({ ...current, url: event.target.value }))
                    }
                    placeholder="https://mcp.example.com/mcp"
                    aria-invalid={formError === "url" || undefined}
                  />
                </div>
                <KeyValueEditor
                  label={t(($) => $.tab_body.mcp_config.dialog_headers_label)}
                  addLabel={t(($) => $.tab_body.mcp_config.dialog_add_header)}
                  removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_header)}
                  rows={form.headers}
                  onChange={(headers) =>
                    setForm((current) => ({ ...current, headers }))
                  }
                />
              </>
            )}
          </TabsContent>

          <TabsContent value="json" className="space-y-2 pt-2">
            {server?.container === "mcp" && (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.tab_body.mcp_config.dialog_native_json_hint)}
              </p>
            )}
            <Label htmlFor="mcp-server-json">
              {t(($) => $.tab_body.mcp_config.dialog_json_label)}
            </Label>
            <Textarea
              id="mcp-server-json"
              name="mcp-server-json"
              autoComplete="off"
              spellCheck={false}
              rows={12}
              className="min-h-64 resize-y font-mono text-caption"
              value={jsonText}
              onChange={(event) => setJsonText(event.target.value)}
              aria-invalid={!jsonResult.ok || undefined}
              aria-label={t(($) => $.tab_body.mcp_config.dialog_json_aria)}
            />
          </TabsContent>
        </Tabs>

        {errorMessage && (
          <p className="text-caption text-destructive" aria-live="polite">
            {errorMessage}
          </p>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
            {t(($) => $.tab_body.mcp_config.dialog_cancel)}
          </Button>
          <Button onClick={handleSave} disabled={!canSave}>
            {saving && (
              <Loader2
                className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                aria-hidden="true"
              />
            )}
            {server
              ? t(($) => $.tab_body.mcp_config.dialog_update_action)
              : t(($) => $.tab_body.mcp_config.dialog_add_action)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StringListEditor({
  label,
  addLabel,
  removeLabel,
  values,
  onChange,
}: {
  label: string;
  addLabel: string;
  removeLabel: string;
  values: string[];
  onChange: (values: string[]) => void;
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="text-body font-medium">{label}</legend>
      {values.map((value, index) => (
        <div key={index} className="flex gap-2">
          <Input
            aria-label={`${label} ${index + 1}`}
            name={`mcp-argument-${index}`}
            autoComplete="off"
            spellCheck={false}
            value={value}
            onChange={(event) =>
              onChange(values.map((item, itemIndex) =>
                itemIndex === index ? event.target.value : item,
              ))
            }
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={`${removeLabel} ${index + 1}`}
            onClick={() => onChange(values.filter((_, itemIndex) => itemIndex !== index))}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" onClick={() => onChange([...values, ""])}>
        <Plus aria-hidden="true" />
        {addLabel}
      </Button>
    </fieldset>
  );
}

function KeyValueEditor({
  label,
  addLabel,
  removeLabel,
  rows,
  onChange,
}: {
  label: string;
  addLabel: string;
  removeLabel: string;
  rows: KeyValue[];
  onChange: (rows: KeyValue[]) => void;
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="text-body font-medium">{label}</legend>
      {rows.map((row, index) => (
        <div key={index} className="grid grid-cols-[1fr_1fr_auto] gap-2">
          <Input
            aria-label={`${label} key ${index + 1}`}
            name={`mcp-pair-key-${index}`}
            autoComplete="off"
            spellCheck={false}
            value={row.key}
            onChange={(event) =>
              onChange(rows.map((item, itemIndex) =>
                itemIndex === index ? { ...item, key: event.target.value } : item,
              ))
            }
            placeholder="Key"
          />
          <Input
            aria-label={`${label} value ${index + 1}`}
            name={`mcp-pair-value-${index}`}
            autoComplete="off"
            spellCheck={false}
            value={row.value}
            onChange={(event) =>
              onChange(rows.map((item, itemIndex) =>
                itemIndex === index ? { ...item, value: event.target.value } : item,
              ))
            }
            placeholder="Value"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={`${removeLabel} ${index + 1}`}
            onClick={() => onChange(rows.filter((_, itemIndex) => itemIndex !== index))}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      ))}
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => onChange([...rows, { key: "", value: "" }])}
      >
        <Plus aria-hidden="true" />
        {addLabel}
      </Button>
    </fieldset>
  );
}

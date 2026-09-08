"use client";

import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { Globe2, Loader2, Plus, SquareTerminal, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@multica/ui/components/ui/field";
import { Input } from "@multica/ui/components/ui/input";
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
  replacementMode = false,
  hideNameWhenEditing = false,
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
  /**
   * Starts from an intentionally blank config and replaces the saved entry.
   * Used by the write-only workspace library, where saved values cannot be
   * read back and renaming is handled separately in the list.
   */
  replacementMode?: boolean;
  /**
   * Keeps configuration editing separate from list-level renaming. The saved
   * name still participates in validation and is passed back unchanged.
   */
  hideNameWhenEditing?: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (name: string, config: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const [name, setName] = useState("");
  const [mode, setMode] = useState<EditorMode>("form");
  const [form, setForm] = useState<McpFormState>(emptyForm);
  const [jsonText, setJsonText] = useState("{}");
  const [saving, setSaving] = useState(false);
  const [saveAttempted, setSaveAttempted] = useState(false);
  const fieldId = useId();
  const nameInputRef = useRef<HTMLInputElement>(null);
  const commandInputRef = useRef<HTMLInputElement>(null);
  const urlInputRef = useRef<HTMLInputElement>(null);
  const jsonInputRef = useRef<HTMLTextAreaElement>(null);
  const formId = `${fieldId}-form`;
  const nameHidden = replacementMode || (hideNameWhenEditing && server !== null);

  useEffect(() => {
    if (!open) return;
    const config = replacementMode ? {} : (server?.config ?? {});
    setName(server?.name ?? "");
    // A caller on an older backend may have only the safe transport summary.
    // Seeding from `formFromConfig({})` there would open an HTTP form for a
    // known stdio server, so take the transport from the summary.
    setForm(
      !server
        ? emptyForm()
        : Object.keys(config).length > 0
          ? formFromConfig(config)
          : { ...emptyForm(), transport: server.transport === "stdio" ? "stdio" : "http" },
    );
    setJsonText(JSON.stringify(config, null, 2));
    setMode(server && !formSupportsServer(server) ? "json" : "form");
    setSaveAttempted(false);
  }, [open, replacementMode, server]);

  // The form is unavailable — not merely unselected — for entries it cannot
  // represent, so switching to it cannot rewrite them either.
  const formAvailable = replacementMode || !server || formSupportsServer(server);

  const jsonResult = useMemo(() => parseServerJson(jsonText), [jsonText]);
  // Existing entries can originate from a runtime or API with a broader name
  // grammar. Configuration edits must retain that identity verbatim; only
  // newly entered names go through the create/rename validation below.
  const preservesServerName = server !== null && name === server.name;
  const submittedName = preservesServerName ? name : name.trim();
  const nameError =
    preservesServerName
      ? null
      : submittedName === ""
        ? "required"
        : !/^[A-Za-z0-9_-]+$/.test(submittedName)
          ? "format"
          : existingNames.has(submittedName) && submittedName !== server?.name
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
  const visibleNameError = saveAttempted ? nameError : null;
  const visibleFormError = saveAttempted ? formError : null;
  const visibleJsonError = saveAttempted && !jsonResult.ok ? jsonResult : null;

  const nameErrorMessage =
    visibleNameError === "required"
      ? t(($) => $.tab_body.mcp_config.dialog_name_required)
      : visibleNameError === "format"
        ? t(($) => $.tab_body.mcp_config.dialog_name_invalid)
        : visibleNameError === "duplicate"
          ? t(($) => $.tab_body.mcp_config.dialog_name_duplicate)
          : "";
  const formErrorMessage =
    mode === "form" && visibleFormError === "command"
      ? t(($) => $.tab_body.mcp_config.dialog_command_required)
      : mode === "form" && visibleFormError === "url"
        ? t(($) => $.tab_body.mcp_config.dialog_url_required)
        : "";
  const jsonErrorMessage =
    mode === "json" && visibleJsonError?.error === "not_object"
      ? t(($) => $.tab_body.mcp_config.dialog_json_object)
      : mode === "json" && visibleJsonError?.error === "missing_target"
        ? t(($) => $.tab_body.mcp_config.dialog_json_target)
        : mode === "json" && visibleJsonError
          ? t(($) => $.tab_body.mcp_config.invalid_json, {
              error: visibleJsonError.error,
            })
          : "";

  const nameHintId = `${fieldId}-name-hint`;
  const nameErrorId = `${fieldId}-name-error`;
  const commandErrorId = `${fieldId}-command-error`;
  const urlErrorId = `${fieldId}-url-error`;
  const jsonErrorId = `${fieldId}-json-error`;

  const handleModeChange = (next: string | number | null) => {
    if (next !== "form" && next !== "json") return;
    if (next === "json" && mode === "form") {
      setJsonText(JSON.stringify(configFromForm(form), null, 2));
    } else if (next === "form" && mode === "json" && jsonResult.ok) {
      setForm(formFromConfig(jsonResult.value));
    }
    setSaveAttempted(false);
    setMode(next);
  };

  const handleSave = async () => {
    setSaveAttempted(true);
    if (
      saving ||
      nameError !== null ||
      (mode === "form" ? formError !== null : !jsonResult.ok)
    ) {
      if (nameError !== null) nameInputRef.current?.focus();
      else if (mode === "json") jsonInputRef.current?.focus();
      else if (formError === "command") commandInputRef.current?.focus();
      else if (formError === "url") urlInputRef.current?.focus();
      return;
    }
    let config: Record<string, unknown>;
    if (mode === "form") config = configFromForm(form);
    else if (jsonResult.ok) config = jsonResult.value;
    else return;
    setSaving(true);
    try {
      await onSave(submittedName, config);
      onOpenChange(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 overscroll-contain p-0 sm:max-w-2xl [&_[data-slot=dialog-close]]:top-5 [&_[data-slot=dialog-close]]:right-5">
            <DialogHeader className="shrink-0 border-b border-surface-border px-6 py-6 pr-14 sm:px-8 sm:pr-16">
              <DialogTitle>
                {replacementMode
                  ? t(($) => $.tab_body.mcp_config.dialog_replace_title, {
                      name: server?.name ?? "",
                    })
                  : server && hideNameWhenEditing
                    ? t(($) => $.tab_body.mcp_config.dialog_edit_title_named, {
                        name: server.name,
                      })
                    : server
                      ? t(($) => $.tab_body.mcp_config.dialog_edit_title)
                      : t(($) => $.tab_body.mcp_config.dialog_add_title)}
              </DialogTitle>
              <DialogDescription>
                {replacementMode
                  ? t(($) => $.tab_body.mcp_config.dialog_replace_description)
                  : t(($) => $.tab_body.mcp_config.dialog_description)}
              </DialogDescription>
            </DialogHeader>

            <form
              id={formId}
              data-slot="mcp-dialog-scroll-area"
              className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-7 sm:px-8 sm:py-8"
              noValidate
              onSubmit={(event) => {
                event.preventDefault();
                void handleSave();
              }}
            >
                <FieldGroup className="gap-8">
                  {!nameHidden ? (
                    <Field>
                      <FieldLabel htmlFor="mcp-server-name">
                        {t(($) => $.tab_body.mcp_config.dialog_name_label)}
                      </FieldLabel>
                      <Input
                        ref={nameInputRef}
                        id="mcp-server-name"
                        name="mcp-server-name"
                        autoFocus
                        required
                        autoComplete="off"
                        spellCheck={false}
                        value={name}
                        onChange={(event) => setName(event.target.value)}
                        aria-invalid={nameErrorMessage ? true : undefined}
                        aria-describedby={nameErrorMessage ? nameErrorId : nameHintId}
                        readOnly={lockName}
                        aria-readonly={lockName || undefined}
                        className={
                          lockName
                            ? "text-muted-foreground aria-invalid:border-input aria-invalid:ring-0"
                            : "aria-invalid:border-input aria-invalid:ring-0"
                        }
                      />
                      {nameErrorMessage ? (
                        <FieldError id={nameErrorId} className="text-caption">
                          {nameErrorMessage}
                        </FieldError>
                      ) : (
                        <FieldDescription id={nameHintId} className="text-caption">
                          {lockName
                            ? t(($) => $.tab_body.mcp_config.dialog_name_locked)
                            : t(($) => $.tab_body.mcp_config.dialog_name_hint)}
                        </FieldDescription>
                      )}
                    </Field>
                  ) : null}

                  <Tabs value={mode} onValueChange={handleModeChange} className="gap-7">
                    <div className="space-y-3">
                      <p className="text-body font-medium">
                        {t(($) => $.tab_body.mcp_config.dialog_editor_label)}
                      </p>
                      <TabsList className="grid w-full grid-cols-2">
                        <TabsTrigger value="form" disabled={!formAvailable}>
                          {t(($) => $.tab_body.mcp_config.dialog_form_tab)}
                        </TabsTrigger>
                        <TabsTrigger value="json">
                          {t(($) => $.tab_body.mcp_config.dialog_json_tab)}
                        </TabsTrigger>
                      </TabsList>
                    </div>

                    <TabsContent value="form" className="space-y-6">
                      <fieldset className="space-y-3">
                        <legend className="text-body font-medium">
                          {t(($) => $.tab_body.mcp_config.dialog_type_label)}
                        </legend>
                        <div className="grid gap-3 sm:grid-cols-2">
                          {(["stdio", "http"] as const).map((transport) => {
                            const selected = form.transport === transport;
                            const Icon = transport === "stdio" ? SquareTerminal : Globe2;
                            return (
                              <button
                                key={transport}
                                type="button"
                                aria-pressed={selected}
                                className={cn(
                                  "flex min-h-16 items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors",
                                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                  selected
                                    ? "border-primary bg-accent/50"
                                    : "border-surface-border hover:bg-accent/30",
                                )}
                                onClick={() => {
                                  setSaveAttempted(false);
                                  setForm((current) => ({ ...current, transport }));
                                }}
                              >
                                <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                                  <Icon aria-hidden="true" />
                                </span>
                                <span className="min-w-0">
                                  <span className="block text-body font-medium text-foreground">
                                    {transport === "stdio"
                                      ? t(($) => $.tab_body.mcp_config.dialog_type_stdio)
                                      : t(($) => $.tab_body.mcp_config.dialog_type_http)}
                                  </span>
                                  <span className="mt-0.5 block text-caption font-normal text-muted-foreground">
                                    {transport === "stdio"
                                      ? t(($) => $.tab_body.mcp_config.dialog_type_stdio_hint)
                                      : t(($) => $.tab_body.mcp_config.dialog_type_http_hint)}
                                  </span>
                                </span>
                              </button>
                            );
                          })}
                        </div>
                      </fieldset>

                      {form.transport === "stdio" ? (
                        <FieldGroup className="gap-7">
                          <Field>
                            <FieldLabel htmlFor="mcp-server-command">
                              {t(($) => $.tab_body.mcp_config.dialog_command_label)}
                            </FieldLabel>
                            <Input
                              ref={commandInputRef}
                              id="mcp-server-command"
                              name="mcp-server-command"
                              // Example CLI executable: a format hint, not translatable copy.
                              // eslint-disable-next-line no-restricted-syntax
                              placeholder="npx"
                              required
                              autoComplete="off"
                              spellCheck={false}
                              value={form.command}
                              onChange={(event) => {
                                setForm((current) => ({
                                  ...current,
                                  command: event.target.value,
                                }));
                              }}
                              aria-invalid={formErrorMessage ? true : undefined}
                              aria-describedby={
                                formErrorMessage ? commandErrorId : undefined
                              }
                              className="aria-invalid:border-input aria-invalid:ring-0"
                            />
                            {formErrorMessage ? (
                              <FieldError id={commandErrorId} className="text-caption">
                                {formErrorMessage}
                              </FieldError>
                            ) : null}
                          </Field>
                          <StringListEditor
                            label={t(($) => $.tab_body.mcp_config.dialog_args_label)}
                            description={t(($) => $.tab_body.mcp_config.dialog_args_hint)}
                            addLabel={t(($) => $.tab_body.mcp_config.dialog_add_arg)}
                            removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_arg)}
                            values={form.args}
                            onChange={(args) =>
                              setForm((current) => ({ ...current, args }))
                            }
                          />
                          <KeyValueEditor
                            label={t(($) => $.tab_body.mcp_config.dialog_env_label)}
                            description={t(($) => $.tab_body.mcp_config.dialog_env_hint)}
                            addLabel={t(($) => $.tab_body.mcp_config.dialog_add_env)}
                            removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_env)}
                            keyLabel={t(($) => $.tab_body.mcp_config.dialog_env_key)}
                            valueLabel={t(($) => $.tab_body.mcp_config.dialog_value)}
                            rows={form.env}
                            onChange={(env) =>
                              setForm((current) => ({ ...current, env }))
                            }
                          />
                        </FieldGroup>
                      ) : (
                        <FieldGroup className="gap-7">
                          <Field>
                            <FieldLabel htmlFor="mcp-server-url">
                              {t(($) => $.tab_body.mcp_config.dialog_url_label)}
                            </FieldLabel>
                            <Input
                              ref={urlInputRef}
                              id="mcp-server-url"
                              name="mcp-server-url"
                              placeholder="https://mcp.example.com/mcp"
                              type="url"
                              inputMode="url"
                              required
                              autoComplete="off"
                              spellCheck={false}
                              value={form.url}
                              onChange={(event) => {
                                setForm((current) => ({
                                  ...current,
                                  url: event.target.value,
                                }));
                              }}
                              aria-invalid={formErrorMessage ? true : undefined}
                              aria-describedby={formErrorMessage ? urlErrorId : undefined}
                              className="aria-invalid:border-input aria-invalid:ring-0"
                            />
                            {formErrorMessage ? (
                              <FieldError id={urlErrorId} className="text-caption">
                                {formErrorMessage}
                              </FieldError>
                            ) : null}
                          </Field>
                          <KeyValueEditor
                            label={t(($) => $.tab_body.mcp_config.dialog_headers_label)}
                            description={t(($) => $.tab_body.mcp_config.dialog_headers_hint)}
                            addLabel={t(($) => $.tab_body.mcp_config.dialog_add_header)}
                            removeLabel={t(($) => $.tab_body.mcp_config.dialog_remove_header)}
                            keyLabel={t(($) => $.tab_body.mcp_config.dialog_header_key)}
                            valueLabel={t(($) => $.tab_body.mcp_config.dialog_value)}
                            rows={form.headers}
                            onChange={(headers) =>
                              setForm((current) => ({ ...current, headers }))
                            }
                          />
                        </FieldGroup>
                      )}
                    </TabsContent>

                    <TabsContent value="json">
                      <Field>
                        <FieldLabel htmlFor="mcp-server-json">
                          {t(($) => $.tab_body.mcp_config.dialog_json_label)}
                        </FieldLabel>
                        {server?.container === "mcp" ? (
                          <FieldDescription className="text-caption">
                            {t(($) => $.tab_body.mcp_config.dialog_native_json_hint)}
                          </FieldDescription>
                        ) : null}
                        <Textarea
                          ref={jsonInputRef}
                          id="mcp-server-json"
                          name="mcp-server-json"
                          autoComplete="off"
                          spellCheck={false}
                          rows={14}
                          className="min-h-72 resize-none font-mono text-caption leading-5 aria-invalid:border-input aria-invalid:ring-0"
                          value={jsonText}
                          onChange={(event) => setJsonText(event.target.value)}
                          aria-invalid={jsonErrorMessage ? true : undefined}
                          aria-describedby={jsonErrorMessage ? jsonErrorId : undefined}
                          aria-label={t(($) => $.tab_body.mcp_config.dialog_json_aria)}
                        />
                        {jsonErrorMessage ? (
                          <FieldError id={jsonErrorId} className="text-caption">
                            {jsonErrorMessage}
                          </FieldError>
                        ) : null}
                      </Field>
                    </TabsContent>
                  </Tabs>
                </FieldGroup>
            </form>

              <div
                data-slot="mcp-dialog-footer"
                className="flex shrink-0 justify-end gap-2 border-t bg-muted/30 px-6 py-3 sm:px-8"
              >
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onOpenChange(false)}
                  disabled={saving}
                >
                  {t(($) => $.tab_body.mcp_config.dialog_cancel)}
                </Button>
                <Button type="submit" size="sm" form={formId} disabled={saving}>
                  {saving ? (
                    <Loader2
                      className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none"
                      aria-hidden="true"
                    />
                  ) : null}
                  {replacementMode
                    ? t(($) => $.tab_body.mcp_config.dialog_replace_action)
                    : server
                    ? t(($) => $.tab_body.mcp_config.dialog_update_action)
                    : t(($) => $.tab_body.mcp_config.dialog_add_action)}
                </Button>
              </div>
      </DialogContent>
    </Dialog>
  );
}

function StringListEditor({
  label,
  description,
  addLabel,
  removeLabel,
  values,
  onChange,
}: {
  label: string;
  description: string;
  addLabel: string;
  removeLabel: string;
  values: string[];
  onChange: (values: string[]) => void;
}) {
  const labelId = useId();
  return (
    <div role="group" aria-labelledby={labelId} className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p id={labelId} className="text-body font-medium">
            {label}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">{description}</p>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="shrink-0"
          onClick={() => onChange([...values, ""])}
        >
          <Plus aria-hidden="true" />
          {addLabel}
        </Button>
      </div>
      {values.length > 0 ? (
        <div className="space-y-2">
          {values.map((value, index) => (
            <div key={index} className="flex items-center gap-2">
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
                size="icon"
                className="shrink-0 text-muted-foreground hover:text-destructive"
                aria-label={`${removeLabel} ${index + 1}`}
                onClick={() =>
                  onChange(values.filter((_, itemIndex) => itemIndex !== index))
                }
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function KeyValueEditor({
  label,
  description,
  addLabel,
  removeLabel,
  keyLabel,
  valueLabel,
  rows,
  onChange,
}: {
  label: string;
  description: string;
  addLabel: string;
  removeLabel: string;
  keyLabel: string;
  valueLabel: string;
  rows: KeyValue[];
  onChange: (rows: KeyValue[]) => void;
}) {
  const labelId = useId();
  return (
    <div role="group" aria-labelledby={labelId} className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <p id={labelId} className="text-body font-medium">
            {label}
          </p>
          <p className="mt-1 text-caption text-muted-foreground">{description}</p>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="shrink-0"
          onClick={() => onChange([...rows, { key: "", value: "" }])}
        >
          <Plus aria-hidden="true" />
          {addLabel}
        </Button>
      </div>
      {rows.length > 0 ? (
        <div className="space-y-2">
          {rows.map((row, index) => (
            <div key={index} className="flex items-start gap-2">
              <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-2">
                <Input
                  aria-label={`${label}: ${keyLabel} ${index + 1}`}
                  name={`mcp-pair-key-${index}`}
                  autoComplete="off"
                  spellCheck={false}
                  value={row.key}
                  onChange={(event) =>
                    onChange(rows.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, key: event.target.value }
                        : item,
                    ))
                  }
                />
                <Input
                  aria-label={`${label}: ${valueLabel} ${index + 1}`}
                  name={`mcp-pair-value-${index}`}
                  autoComplete="off"
                  spellCheck={false}
                  value={row.value}
                  onChange={(event) =>
                    onChange(rows.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, value: event.target.value }
                        : item,
                    ))
                  }
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="shrink-0 text-muted-foreground hover:text-destructive"
                aria-label={`${removeLabel} ${index + 1}`}
                onClick={() =>
                  onChange(rows.filter((_, itemIndex) => itemIndex !== index))
                }
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

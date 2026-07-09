"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  OPENCLAW_GATEWAY_TOKEN_MASK,
  type OpenclawRoutingMode,
  type OpenclawRuntimeConfig,
  openclawRuntimeConfigEquals,
  parseOpenclawRuntimeConfig,
  serializeOpenclawRuntimeConfig,
} from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../../i18n";

// Form state mirrors OpenclawRuntimeConfig, but always carries a defined
// mode value so the radio group is fully controlled. Empty-string mode
// shouldn't be reachable in this form because the field defaults to "local"
// the first time an openclaw agent's tab opens — equivalent to the daemon's
// fail-soft default — but the union accepts it as a defensive belt.
interface FormState {
  mode: OpenclawRoutingMode;
  host: string;
  port: string;
  token: string;
  tls: boolean;
  // tokenWasMasked records whether the form opened against a persisted token
  // (server responded with the mask sentinel). It tracks "the user has not
  // touched the token field since open" so submit can replay the sentinel
  // back to the server, which the matching preserve hook treats as "keep
  // the persisted value". Any user keystroke clears the flag, at which
  // point token is taken at face value.
  tokenWasMasked: boolean;
}

function configToForm(cfg: OpenclawRuntimeConfig): FormState {
  const masked = cfg.gateway?.token === OPENCLAW_GATEWAY_TOKEN_MASK;
  return {
    mode: cfg.mode ?? "local",
    host: cfg.gateway?.host ?? "",
    port: cfg.gateway?.port ? String(cfg.gateway.port) : "",
    // Never display the mask sentinel in the input — that would let users
    // accidentally edit it. Show an empty field with a placeholder hint
    // instead, and remember the masked state separately.
    token: masked ? "" : (cfg.gateway?.token ?? ""),
    tls: cfg.gateway?.tls === true,
    tokenWasMasked: masked,
  };
}

function formToConfig(state: FormState): OpenclawRuntimeConfig {
  const cfg: OpenclawRuntimeConfig = { mode: state.mode };
  if (state.mode === "gateway") {
    const gw: NonNullable<OpenclawRuntimeConfig["gateway"]> = {};
    if (state.host.trim() !== "") gw.host = state.host.trim();
    const portNum = Number.parseInt(state.port, 10);
    if (Number.isFinite(portNum) && portNum > 0) gw.port = portNum;
    if (state.tls) gw.tls = true;
    if (state.tokenWasMasked && state.token === "") {
      // User opened a saved token and never touched the field — replay
      // the mask sentinel so the server's preserve hook keeps the
      // persisted value. The matching client-side serializer drops the
      // sentinel before it hits the wire, but we need it on the typed
      // object so the equality check sees an unchanged config.
      gw.token = OPENCLAW_GATEWAY_TOKEN_MASK;
    } else if (state.token !== "") {
      gw.token = state.token;
    }
    if (Object.keys(gw).length > 0) cfg.gateway = gw;
  }
  return cfg;
}

export function RuntimeConfigTab({
  agent,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  onSave: (updates: { runtime_config: Record<string, unknown> }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");

  const original = useMemo<OpenclawRuntimeConfig>(
    () => parseOpenclawRuntimeConfig(agent.runtime_config),
    [agent.runtime_config],
  );
  const originalForm = useMemo(() => configToForm(original), [original]);

  const [state, setState] = useState<FormState>(originalForm);
  const [saving, setSaving] = useState(false);

  // Sync local draft when the agent prop changes — same pattern as
  // McpConfigTab. Only adopt the new server value when the user has no
  // in-flight edits relative to the *previous* original.
  const previousFormRef = useRef(originalForm);
  useEffect(() => {
    setState((current) =>
      formEquals(current, previousFormRef.current) ? originalForm : current,
    );
    previousFormRef.current = originalForm;
  }, [originalForm]);

  const currentCfg = useMemo(() => formToConfig(state), [state]);
  const dirty = !openclawRuntimeConfigEquals(original, currentCfg);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const portValid = state.port === "" || /^\d+$/.test(state.port);
  const canSave = portValid && !saving;

  const handleSave = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      await onSave({
        runtime_config: serializeOpenclawRuntimeConfig(currentCfg),
      });
      toast.success(t(($) => $.tab_body.runtime_config.saved_toast));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.runtime_config.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  const isGateway = state.mode === "gateway";

  return (
    <div className="flex h-full flex-col space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.runtime_config.intro)}
      </p>

      <fieldset className="space-y-2">
        <Label className="text-xs font-medium">
          {t(($) => $.tab_body.runtime_config.mode_label)}
        </Label>
        <div className="flex gap-2">
          {(["local", "gateway"] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              onClick={() =>
                setState((s) => {
                  if (s.mode === mode) return s;
                  // Switching modes clears `tokenWasMasked`. Without this
                  // a user who flips gateway → local → gateway would
                  // re-arm the "replay the mask sentinel back to the
                  // server" branch in formToConfig, silently restoring
                  // a token they had no intent of keeping (see CR for
                  // issue #3260).
                  return { ...s, mode, tokenWasMasked: false };
                })
              }
              className={`rounded-md border px-3 py-1.5 text-xs ${
                state.mode === mode
                  ? "border-foreground bg-foreground text-background"
                  : "border-border bg-background text-foreground hover:bg-muted"
              }`}
            >
              {t(($) => $.tab_body.runtime_config[`mode_${mode}`])}
            </button>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          {isGateway
            ? t(($) => $.tab_body.runtime_config.mode_gateway_hint)
            : t(($) => $.tab_body.runtime_config.mode_local_hint)}
        </p>
      </fieldset>

      <fieldset
        className={`space-y-3 rounded-md border p-3 ${isGateway ? "" : "opacity-50"}`}
        disabled={!isGateway}
      >
        <legend className="px-1 text-xs font-medium">
          {t(($) => $.tab_body.runtime_config.gateway_legend)}
        </legend>

        <div className="space-y-1.5">
          <Label htmlFor="openclaw-gw-host" className="text-xs">
            {t(($) => $.tab_body.runtime_config.host_label)}
          </Label>
          <Input
            id="openclaw-gw-host"
            value={state.host}
            onChange={(e) => setState((s) => ({ ...s, host: e.target.value }))}
            placeholder={t(($) => $.tab_body.runtime_config.host_placeholder)}
            className="font-mono text-xs"
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="openclaw-gw-port" className="text-xs">
            {t(($) => $.tab_body.runtime_config.port_label)}
          </Label>
          <Input
            id="openclaw-gw-port"
            value={state.port}
            onChange={(e) => setState((s) => ({ ...s, port: e.target.value }))}
            placeholder="18789"
            inputMode="numeric"
            aria-invalid={!portValid || undefined}
            className="font-mono text-xs"
          />
          {!portValid && (
            <p className="text-xs text-destructive">
              {t(($) => $.tab_body.runtime_config.port_invalid)}
            </p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="openclaw-gw-token" className="text-xs">
            {t(($) => $.tab_body.runtime_config.token_label)}
          </Label>
          <Input
            id="openclaw-gw-token"
            type="password"
            value={state.token}
            onChange={(e) =>
              setState((s) => ({
                ...s,
                token: e.target.value,
                // The user touched the field — drop the masked-replay
                // flag so a subsequent empty input genuinely clears the
                // persisted token instead of preserving it.
                tokenWasMasked: false,
              }))
            }
            placeholder={
              state.tokenWasMasked
                ? t(($) => $.tab_body.runtime_config.token_masked_placeholder)
                : t(($) => $.tab_body.runtime_config.token_placeholder)
            }
            autoComplete="off"
            className="font-mono text-xs"
          />
        </div>

        <div className="flex items-center justify-between gap-2 pt-1">
          <div>
            <Label htmlFor="openclaw-gw-tls" className="text-xs">
              {t(($) => $.tab_body.runtime_config.tls_label)}
            </Label>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.runtime_config.tls_hint)}
            </p>
          </div>
          <Switch
            id="openclaw-gw-tls"
            checked={state.tls}
            // Explicit `disabled` mirrors the surrounding fieldset's state.
            // The native `<fieldset disabled>` attribute only deactivates
            // built-in form controls; @base-ui's Switch is an ARIA component
            // and stays interactive unless we tell it otherwise. Without
            // this guard users could flip TLS on while still in Local mode.
            disabled={!isGateway}
            onCheckedChange={(checked: boolean) =>
              setState((s) => ({ ...s, tls: checked }))
            }
          />
        </div>
      </fieldset>

      <div className="flex items-center justify-end gap-3 pt-2">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || !canSave} size="sm">
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

function formEquals(a: FormState, b: FormState): boolean {
  return (
    a.mode === b.mode &&
    a.host === b.host &&
    a.port === b.port &&
    a.token === b.token &&
    a.tls === b.tls &&
    a.tokenWasMasked === b.tokenWasMasked
  );
}

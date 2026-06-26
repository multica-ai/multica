import { useCallback, useMemo, useState } from "react";
import { AlertCircle, Check, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

// FIR-2037: server picker shared by the Settings → Server tab and the login
// screen (so a user can switch BEFORE logging in — the settings tab is behind
// auth, which is a trap when the configured server is unreachable).
//
// The packaged app reads which backend to talk to from ~/.multica/desktop.json
// (see runtime-config-loader.ts). Production's API host is the dedicated backend
// `multica-api.firtal.com`, NOT the web host `multica.firtal.com` (the latter is
// the Next.js frontend behind Cloudflare Access; direct API/auth/ws calls there
// fail). The web login page still lives at multica.firtal.com — that's `appUrl`.
//
// Strings are hardcoded English (the app is English) to keep this self-contained
// in apps/desktop without touching the shared locale files.

interface Preset {
  id: string;
  label: string;
  apiUrl: string;
  /** Web URL for the login page / deep links. Omit to derive from apiUrl. */
  appUrl?: string;
}

const PRESETS: Preset[] = [
  {
    id: "production",
    label: "Production",
    apiUrl: "https://multica-api.firtal.com",
    appUrl: "https://multica.firtal.com",
  },
  {
    id: "staging",
    label: "Staging",
    apiUrl: "https://sara.firtal.com",
    appUrl: "https://sara.firtal.com",
  },
];

type SaveState =
  | { status: "idle" }
  | { status: "saving" }
  | { status: "error"; message: string };

function normalize(url: string): string {
  return url.trim().replace(/\/+$/, "").toLowerCase();
}

export function ServerPicker() {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  const currentApiUrl = runtimeConfig.ok ? runtimeConfig.config.apiUrl : "";

  const matchedPreset = useMemo(
    () => PRESETS.find((p) => normalize(p.apiUrl) === normalize(currentApiUrl)),
    [currentApiUrl],
  );

  const [choice, setChoice] = useState<string>(matchedPreset?.id ?? "custom");
  const [customUrl, setCustomUrl] = useState<string>(
    matchedPreset ? "" : currentApiUrl,
  );
  const [state, setState] = useState<SaveState>({ status: "idle" });

  const selectedPreset = PRESETS.find((p) => p.id === choice);
  const targetApiUrl =
    choice === "custom" ? customUrl.trim() : (selectedPreset?.apiUrl ?? "");
  const targetAppUrl = choice === "custom" ? undefined : selectedPreset?.appUrl;

  const isUnchanged = normalize(targetApiUrl) === normalize(currentApiUrl);
  const canSave =
    state.status !== "saving" && targetApiUrl.length > 0 && !isUnchanged;

  const handleSave = useCallback(async () => {
    if (!targetApiUrl) return;
    setState({ status: "saving" });
    const result = await window.desktopAPI.setRuntimeConfig(
      targetApiUrl,
      targetAppUrl,
    );
    if (!result.ok) {
      setState({ status: "error", message: result.error });
      return;
    }
    // Config written — relaunch so main reloads endpoints from desktop.json.
    await window.desktopAPI.relaunchApp();
  }, [targetApiUrl, targetAppUrl]);

  return (
    <div className="space-y-3">
      <div>
        <p className="text-sm font-medium">Current server</p>
        <p className="text-sm text-muted-foreground mt-0.5 font-mono break-all">
          {currentApiUrl || "—"}
        </p>
      </div>

      <div className="space-y-3 pt-1">
        <p className="text-sm font-medium">Connect to</p>

        {PRESETS.map((preset) => (
          <label
            key={preset.id}
            className="flex items-start gap-3 cursor-pointer"
          >
            <input
              type="radio"
              name="server-choice"
              className="mt-1"
              checked={choice === preset.id}
              onChange={() => setChoice(preset.id)}
            />
            <span className="min-w-0">
              <span className="block text-sm font-medium">{preset.label}</span>
              <span className="block text-sm text-muted-foreground font-mono break-all">
                {preset.apiUrl}
              </span>
            </span>
          </label>
        ))}

        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="radio"
            name="server-choice"
            className="mt-1"
            checked={choice === "custom"}
            onChange={() => setChoice("custom")}
          />
          <span className="min-w-0 flex-1">
            <span className="block text-sm font-medium">Custom URL</span>
            <Input
              type="url"
              placeholder="https://your-api.example.com"
              value={customUrl}
              onFocus={() => setChoice("custom")}
              onChange={(e) => setCustomUrl(e.target.value)}
              className="mt-1.5 font-mono"
            />
          </span>
        </label>

        {state.status === "error" && (
          <p className="text-sm text-destructive inline-flex items-center gap-1.5">
            <AlertCircle className="size-3.5" />
            {state.message}
          </p>
        )}

        <div className="flex items-center gap-3 pt-1">
          <Button size="sm" onClick={handleSave} disabled={!canSave}>
            {state.status === "saving" ? (
              <>
                <Loader2 className="size-3.5 animate-spin" />
                Saving…
              </>
            ) : (
              "Save & restart"
            )}
          </Button>
          {isUnchanged && targetApiUrl.length > 0 && (
            <span className="text-sm text-muted-foreground inline-flex items-center gap-1.5">
              <Check className="size-3.5 text-success" />
              Already connected
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

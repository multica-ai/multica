import { useCallback, useMemo, useState } from "react";
import { AlertCircle, Check, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

// FIR-2037: desktop "Server" setting. The packaged app reads which backend to
// talk to from ~/.multica/desktop.json (see runtime-config-loader.ts). Before
// this tab the only way to switch was hand-editing that file; now the user
// picks Production / Staging / a custom URL and the app writes the config and
// relaunches itself. Strings are hardcoded English (the app is English) to keep
// this self-contained in apps/desktop without touching the shared locale files.

const PRESETS = [
  { id: "production", label: "Production", apiUrl: "https://multica.firtal.com" },
  { id: "staging", label: "Staging", apiUrl: "https://sara.firtal.com" },
] as const;

type SaveState =
  | { status: "idle" }
  | { status: "saving" }
  | { status: "error"; message: string };

function normalize(url: string): string {
  return url.trim().replace(/\/+$/, "").toLowerCase();
}

export function ServerSettingsTab() {
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

  const targetApiUrl =
    choice === "custom"
      ? customUrl.trim()
      : (PRESETS.find((p) => p.id === choice)?.apiUrl ?? "");

  const isUnchanged = normalize(targetApiUrl) === normalize(currentApiUrl);
  const canSave =
    state.status !== "saving" && targetApiUrl.length > 0 && !isUnchanged;

  const handleSave = useCallback(async () => {
    if (!targetApiUrl) return;
    setState({ status: "saving" });
    const result = await window.desktopAPI.setRuntimeConfig(targetApiUrl);
    if (!result.ok) {
      setState({ status: "error", message: result.error });
      return;
    }
    // Config written — relaunch so main reloads endpoints from desktop.json.
    await window.desktopAPI.relaunchApp();
  }, [targetApiUrl]);

  return (
    <div>
      <h2 className="text-lg font-semibold">Server</h2>
      <p className="text-sm text-muted-foreground mt-1">
        Choose which Multica server this app connects to. Changing it signs you
        out of the current server and restarts the app.
      </p>

      <div className="mt-6 divide-y">
        <div className="flex items-center justify-between gap-6 py-4">
          <div className="min-w-0">
            <p className="text-sm font-medium">Current server</p>
            <p className="text-sm text-muted-foreground mt-0.5 font-mono break-all">
              {currentApiUrl || "—"}
            </p>
          </div>
        </div>

        <div className="py-4 space-y-3">
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
                placeholder="https://your-server.example.com"
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
                Already connected to this server
              </span>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

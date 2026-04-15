import { useState, useEffect, useCallback } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import type { DaemonPrefs } from "../../../shared/daemon-types";

function SettingRow({
  label,
  description,
  children,
}: {
  label: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-6 py-4">
      <div className="min-w-0">
        <p className="text-sm font-medium">{label}</p>
        <p className="text-sm text-muted-foreground mt-0.5">{description}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

export function DaemonSettingsTab() {
  const [prefs, setPrefs] = useState<DaemonPrefs>({ autoStart: true, autoStop: false });
  const [cliInstalled, setCliInstalled] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    window.daemonAPI.getPrefs().then(setPrefs);
    window.daemonAPI.isCliInstalled().then(setCliInstalled);
  }, []);

  const updatePref = useCallback(
    async (key: keyof DaemonPrefs, value: boolean) => {
      setSaving(true);
      const updated = await window.daemonAPI.setPrefs({ [key]: value });
      setPrefs(updated);
      setSaving(false);
    },
    [],
  );

  return (
    <div>
      <h2 className="text-lg font-semibold">Daemon</h2>
      <p className="text-sm text-muted-foreground mt-1">
        Configure how the local agent daemon behaves with the desktop app.
      </p>

      <div className="mt-6 divide-y">
        <SettingRow
          label="Auto-start on launch"
          description="Automatically start the daemon when the app opens and you are logged in."
        >
          <Switch
            checked={prefs.autoStart}
            onCheckedChange={(checked) => updatePref("autoStart", checked)}
            disabled={saving}
          />
        </SettingRow>

        <SettingRow
          label="Auto-stop on quit"
          description="Stop the daemon when the desktop app is closed. Disable this to keep the daemon running in the background."
        >
          <Switch
            checked={prefs.autoStop}
            onCheckedChange={(checked) => updatePref("autoStop", checked)}
            disabled={saving}
          />
        </SettingRow>

        <div className="py-4">
          <p className="text-sm font-medium">CLI Status</p>
          <p className="text-sm text-muted-foreground mt-1">
            {cliInstalled === null
              ? "Checking…"
              : cliInstalled
                ? "multica CLI is installed and available in PATH."
                : "multica CLI not found. Install it to enable daemon management."}
          </p>
          {cliInstalled === false && (
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() =>
                window.desktopAPI.openExternal(
                  "https://github.com/multica-ai/multica#cli-installation",
                )
              }
            >
              Installation Guide
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

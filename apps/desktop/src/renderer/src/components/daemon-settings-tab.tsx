import { useState, useEffect, useCallback, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "@multica/i18n/react";
import type { DaemonPrefs, DaemonStatus } from "../../../shared/daemon-types";
import {
  DAEMON_STATE_COLORS,
  getDaemonStateKeys,
  formatUptime,
} from "../../../shared/daemon-types";

function SettingRow({
  label,
  description,
  children,
}: {
  label: string;
  description: string;
  children: ReactNode;
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

// One row inside the diagnostics block. Values that are likely to be
// long IDs / URLs render as monospaced + truncated with a tooltip.
function DiagnosticsRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="grid grid-cols-[140px_minmax(0,1fr)] items-baseline gap-3 py-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span
        className={cn(
          "min-w-0 truncate text-sm",
          mono && "font-mono text-xs",
        )}
        title={typeof value === "string" ? value : undefined}
      >
        {value}
      </span>
    </div>
  );
}

export function DaemonSettingsTab() {
  const t = useT("desktop");
  const [prefs, setPrefs] = useState<DaemonPrefs>({ autoStart: true, autoStop: false });
  const [cliInstalled, setCliInstalled] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });

  useEffect(() => {
    window.daemonAPI.getPrefs().then(setPrefs);
    window.daemonAPI.isCliInstalled().then(setCliInstalled);
    window.daemonAPI.getStatus().then(setStatus);
    return window.daemonAPI.onStatusChange(setStatus);
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
      <h2 className="text-lg font-semibold">{t("settings_daemon_title")}</h2>
      <p className="text-sm text-muted-foreground mt-1">
        {t("settings_daemon_desc")}
      </p>

      <div className="mt-6 divide-y">
        <SettingRow
          label={t("settings_auto_start")}
          description={t("settings_auto_start_desc")}
        >
          <Switch
            checked={prefs.autoStart}
            onCheckedChange={(checked) => updatePref("autoStart", checked)}
            disabled={saving}
          />
        </SettingRow>

        <SettingRow
          label={t("settings_auto_stop")}
          description={t("settings_auto_stop_desc")}
        >
          <Switch
            checked={prefs.autoStop}
            onCheckedChange={(checked) => updatePref("autoStop", checked)}
            disabled={saving}
          />
        </SettingRow>

        <div className="py-4">
          <p className="text-sm font-medium">{t("settings_cli_status")}</p>
          <p className="text-sm text-muted-foreground mt-1">
            {cliInstalled === null
              ? t("settings_cli_checking")
              : cliInstalled
                ? t("settings_cli_installed")
                : t("settings_cli_not_found")}
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
              {t("settings_installation_guide")}
            </Button>
          )}
        </div>
      </div>

      {/* Diagnostics — moved out of the logs panel so the panel can focus
          on logs. These fields matter for support tickets and bug reports,
          not for everyday use. */}
      <div className="mt-8">
        <h3 className="text-sm font-semibold">{t("settings_diagnostics")}</h3>
        <p className="text-xs text-muted-foreground mt-1">
          {t("settings_diagnostics_desc")}
        </p>
        <div className="mt-3 rounded-lg border bg-muted/20 px-4 py-2">
          <DiagnosticsRow
            label={t("settings_diag_state")}
            value={
              <span className="inline-flex items-center gap-1.5">
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    DAEMON_STATE_COLORS[status.state],
                  )}
                />
                {t(getDaemonStateKeys(status.state))}
              </span>
            }
          />
          <DiagnosticsRow
            label={t("settings_diag_uptime")}
            value={status.uptime ? formatUptime(status.uptime) : "—"}
          />
          <DiagnosticsRow
            label={t("settings_diag_pid")}
            value={status.pid ?? "—"}
            mono={!!status.pid}
          />
          <DiagnosticsRow
            label={t("settings_diag_daemon_id")}
            value={status.daemonId ?? "—"}
            mono={!!status.daemonId}
          />
          <DiagnosticsRow
            label={t("settings_diag_profile")}
            value={status.profile || "default"}
          />
          <DiagnosticsRow
            label={t("settings_diag_server_url")}
            value={status.serverUrl ?? "—"}
            mono={!!status.serverUrl}
          />
          <DiagnosticsRow
            label={t("settings_diag_device")}
            value={status.deviceName ?? "—"}
          />
          <DiagnosticsRow
            label={t("settings_diag_workspaces")}
            value={
              typeof status.workspaceCount === "number"
                ? status.workspaceCount
                : "—"
            }
          />
        </div>
      </div>
    </div>
  );
}

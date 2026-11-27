import { useCallback, useEffect, useRef, useState } from "react";
import { AlertCircle, ArrowDownToLine, Check, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "@multica/views/i18n";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";
import { toast } from "sonner";
import type { UpdaterDownloadState, UpdaterFailureReason } from "../../../shared/updater-types";

type CheckState =
  | { status: "idle" }
  | { status: "checking" }
  | { status: "up-to-date" }
  | { status: "available"; latestVersion: string }
  | { status: "downloaded"; latestVersion: string }
  | { status: "error"; reason: UpdaterFailureReason; downloading?: boolean };

function downloadCheckState(state: UpdaterDownloadState): CheckState {
  switch (state.status) {
    case "downloading": return { status: "available", latestVersion: state.version };
    case "downloaded": return { status: "downloaded", latestVersion: state.version };
    case "error": return { status: "error", reason: state.reason, downloading: true };
    default: return { status: "idle" };
  }
}

export function UpdatesSettingsTab() {
  const { t } = useT("settings");
  const [state, setState] = useState<CheckState>({ status: "idle" });
  const [automaticUpdates, setAutomaticUpdates] = useState(true);
  const [preferencesReady, setPreferencesReady] = useState(false);
  const [savingPreference, setSavingPreference] = useState(false);
  const currentVersion = window.desktopAPI.appInfo.version;
  const downloadRevision = useRef(0);

  useEffect(() => {
    let mounted = true;
    let receivedEvent = false;
    const unsubscribe = window.updater.onDownloadState((download) => {
      receivedEvent = true;
      if (download.status === "idle") return;
      downloadRevision.current++;
      setState((previous) => {
        // Automatic failures stay quiet unless this page is following a check
        // or download. A ready download still uses the existing install banner.
        if (download.status === "error" && previous.status !== "checking" && previous.status !== "available") return previous;
        return downloadCheckState(download);
      });
    });
    void window.updater.getDownloadState().then((download) => {
      if (mounted && !receivedEvent && (download.status === "downloading" || download.status === "downloaded")) {
        setState(downloadCheckState(download));
      }
    }).catch(() => { /* A manual check can still recover when IPC is available. */ });
    return () => { mounted = false; unsubscribe(); };
  }, []);

  useEffect(() => {
    let mounted = true;
    void window.updater
      .getPreferences()
      .then((preferences) => {
        if (mounted) setAutomaticUpdates(preferences.automaticUpdates);
      })
      .catch(() => {
        // The main process falls back to enabled when preferences cannot be
        // read. Keep the same safe default if IPC itself becomes unavailable.
      })
      .finally(() => {
        if (mounted) setPreferencesReady(true);
      });

    return () => {
      mounted = false;
    };
  }, []);

  const handleAutomaticUpdatesChange = useCallback(
    async (enabled: boolean) => {
      setSavingPreference(true);
      try {
        const preferences = await window.updater.setAutomaticUpdates(enabled);
        setAutomaticUpdates(preferences.automaticUpdates);
        toast.success(t(($) => $.auto_save.toast_saved), {
          id: "settings-auto-save",
        });
      } catch {
        toast.error(t(($) => $.desktop.updates.automatic_updates_save_failed));
      } finally {
        setSavingPreference(false);
      }
    },
    [t],
  );

  const handleCheck = useCallback(async () => {
    setState({ status: "checking" });
    const revision = downloadRevision.current;
    try {
      const result = await window.updater.checkForUpdates();
      // A download event can arrive before the metadata IPC response.
      if (revision !== downloadRevision.current) return;
      if (!result.ok) {
        setState({ status: "error", reason: result.reason });
        return;
      }
      setState(result.downloadState.status === "downloaded" || (result.available && result.downloadState.status !== "idle")
        ? downloadCheckState(result.downloadState)
        : result.available
          ? { status: "available", latestVersion: result.latestVersion }
          : { status: "up-to-date" });
    } catch {
      if (revision === downloadRevision.current) setState({ status: "error", reason: "unknown" });
    }
  }, []);

  const failureMessage = state.status !== "error" ? ""
    : state.downloading ? t(($) => $.desktop.updates.download_failed)
      : state.reason === "network" ? t(($) => $.desktop.updates.check_network)
        : state.reason === "unavailable" ? t(($) => $.desktop.updates.check_unavailable)
          : t(($) => $.desktop.updates.check_failed);

  return (
    <SettingsTab
      title={t(($) => $.desktop.updates.title)}
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.desktop.updates.current_version)}>
          <span className="font-mono text-caption text-muted-foreground">
            v{currentVersion}
          </span>
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.updates.automatic_updates_title)}
          description={t(($) => $.desktop.updates.automatic_updates_description)}
        >
          <Switch
            checked={automaticUpdates}
            onCheckedChange={handleAutomaticUpdatesChange}
            disabled={!preferencesReady || savingPreference}
            aria-label={t(($) => $.desktop.updates.automatic_updates_title)}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.updates.check_section_title)}
          align="start"
          description={
            <>
              <p>{t(($) => $.desktop.updates.check_section_description)}</p>
              {state.status === "up-to-date" && (
                <p className="mt-2 inline-flex items-center gap-1.5">
                  <Check className="size-3.5 text-success" />
                  {t(($) => $.desktop.updates.up_to_date)}
                </p>
              )}
              {state.status === "available" && (
                <p className="mt-2 inline-flex items-center gap-1.5">
                  <ArrowDownToLine className="size-3.5 text-primary" />
                  {t(($) => $.desktop.updates.downloading, {
                    version: state.latestVersion,
                  })}
                </p>
              )}
              {state.status === "error" && (
                <p role="status" className="mt-2 inline-flex items-center gap-1.5">
                  <AlertCircle className="size-3.5 shrink-0" />
                  {failureMessage}
                </p>
              )}
              {state.status === "downloaded" && (
                <p role="status" className="mt-2 inline-flex items-center gap-1.5">
                  <Check className="size-3.5 shrink-0 text-success" />
                  {t(($) => $.desktop.updates.download_ready, { version: state.latestVersion })}
                </p>
              )}
            </>
          }
        >
          <Button
            variant="outline"
            size="sm"
            onClick={handleCheck}
            disabled={state.status === "checking" || state.status === "available"}
            aria-busy={state.status === "checking"}
          >
            {state.status === "checking" ? (
              <>
                <Loader2 className="size-3.5 animate-spin" />
                {t(($) => $.desktop.updates.checking)}
              </>
            ) : (
              state.status === "error" ? t(($) => $.desktop.updates.retry) : t(($) => $.desktop.updates.check_now)
            )}
          </Button>
        </SettingsRow>
      </SettingsCard>
    </SettingsTab>
  );
}

import { useCallback, useEffect, useState } from "react";
import { AlertCircle, Check, Loader2, RefreshCw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "@multica/views/i18n";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";
import { toast } from "sonner";

type TestState =
  | { status: "idle" }
  | { status: "testing" }
  | { status: "success" }
  | { status: "error"; message: string };

export function NetworkSettingsTab() {
  const { t } = useT("settings");
  const [proxyUrl, setProxyUrl] = useState("");
  const [saving, setSaving] = useState(false);
  const [testState, setTestState] = useState<TestState>({ status: "idle" });

  // Load current proxy URL from main process on mount.
  useEffect(() => {
    let mounted = true;
    void window.desktopAPI.proxy.get().then((url) => {
      if (mounted) setProxyUrl(url ?? "");
    });
    return () => {
      mounted = false;
    };
  }, []);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const trimmed = proxyUrl.trim();
      const result = await window.desktopAPI.proxy.set(trimmed || null);
      if (!result.ok) {
        toast.error(result.error ?? t(($) => $.desktop.network.proxy_save_failed));
        return;
      }
      toast.success(t(($) => $.desktop.network.proxy_saved), {
        id: "proxy-saved",
      });
    } catch {
      toast.error(t(($) => $.desktop.network.proxy_save_failed));
    } finally {
      setSaving(false);
    }
  }, [proxyUrl, t]);

  const handleTest = useCallback(async () => {
    const trimmed = proxyUrl.trim();
    if (!trimmed) return;
    setTestState({ status: "testing" });
    try {
      const result = await window.desktopAPI.proxy.test(trimmed);
      if (result.ok) {
        setTestState({ status: "success" });
      } else {
        setTestState({
          status: "error",
          message: result.error ?? "Connection failed",
        });
      }
    } catch {
      setTestState({ status: "error", message: "Unexpected error" });
    }
  }, [proxyUrl]);

  return (
    <SettingsTab title={t(($) => $.desktop.network.title)}>
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.network.proxy_title)}
          description={t(($) => $.desktop.network.proxy_description)}
        >
          <Input
            value={proxyUrl}
            onChange={(e) => setProxyUrl(e.target.value)}
            placeholder={t(($) => $.desktop.network.proxy_placeholder)}
            disabled={saving}
          />
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.network.test_button)}
          description={
            <>
              {testState.status === "success" && (
                <p className="mt-1 inline-flex items-center gap-1.5">
                  <Check className="size-3.5 text-success" />
                  {t(($) => $.desktop.network.test_success)}
                </p>
              )}
              {testState.status === "error" && (
                <p className="mt-1 inline-flex items-center gap-1.5 text-destructive">
                  <AlertCircle className="size-3.5" />
                  {testState.message}
                </p>
              )}
            </>
          }
        >
          <Button
            variant="outline"
            size="sm"
            onClick={handleTest}
            disabled={!proxyUrl.trim() || testState.status === "testing"}
          >
            {testState.status === "testing" ? (
              <>
                <Loader2 className="size-3.5 animate-spin" />
                {t(($) => $.desktop.network.testing)}
              </>
            ) : (
              <>
                <RefreshCw className="size-3.5" />
                {t(($) => $.desktop.network.test_button)}
              </>
            )}
          </Button>
        </SettingsRow>

        <SettingsRow label="">
          <Button
            size="sm"
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              t(($) => $.desktop.network.save_button)
            )}
          </Button>
        </SettingsRow>
      </SettingsCard>

      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.network.restart_notice_title)}
          description={t(($) => $.desktop.network.restart_notice)}
        >
          <span />
        </SettingsRow>
      </SettingsCard>
    </SettingsTab>
  );
}

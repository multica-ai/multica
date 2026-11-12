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

function getProxyParts(value: string | null): {
  host: string;
  username: string;
  password: string;
  hasStoredPassword: boolean;
} {
  if (!value) {
    return { host: "", username: "", password: "", hasStoredPassword: false };
  }

  try {
    const url = new URL(value);
    const hasStoredPassword = Boolean(url.password);
    return {
      host: `${url.protocol}//${url.host}`,
      username: url.username,
      password: url.password === "********" ? "" : url.password,
      hasStoredPassword,
    };
  } catch {
    return { host: value, username: "", password: "", hasStoredPassword: false };
  }
}

function buildProxyUrl(host: string, username: string, password: string, keepExistingPassword: boolean): string {
  const trimmedHost = host.trim();
  if (!trimmedHost) return "";
  const url = new URL(trimmedHost);
  if (username.trim()) {
    url.username = username.trim();
    url.password = keepExistingPassword ? "********" : password.trim();
  } else {
    url.username = "";
    url.password = "";
  }
  return url.toString();
}

export function NetworkSettingsTab() {
  const { t } = useT("settings");
  const [proxyUrl, setProxyUrl] = useState("");
  const [proxyUsername, setProxyUsername] = useState("");
  const [proxyPassword, setProxyPassword] = useState("");
  const [keepExistingPassword, setKeepExistingPassword] = useState(false);
  const [noProxy, setNoProxy] = useState("");
  const [saving, setSaving] = useState(false);
  const [testState, setTestState] = useState<TestState>({ status: "idle" });

  // Load current proxy settings from main process on mount.
  useEffect(() => {
    let mounted = true;
    void window.desktopAPI.proxy.get().then((cfg) => {
      if (mounted) {
        const parts = getProxyParts(cfg.proxyUrl ?? null);
        setProxyUrl(parts.host);
        setProxyUsername(parts.username);
        setProxyPassword(parts.password);
        setKeepExistingPassword(parts.hasStoredPassword && !parts.password);
        setNoProxy(cfg.noProxy ?? "");
      }
    });
    return () => {
      mounted = false;
    };
  }, []);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const trimmedNoProxy = noProxy.trim();
      const nextProxyUrl = buildProxyUrl(proxyUrl, proxyUsername, proxyPassword, keepExistingPassword);
      const result = await window.desktopAPI.proxy.set(
        nextProxyUrl || null,
        trimmedNoProxy || null,
      );
      if (!result.ok) {
        if (result.cancelled) return;
        toast.error(result.error ?? t(($) => $.desktop.network.proxy_save_failed));
        return;
      }
      const saved = await window.desktopAPI.proxy.get();
      const parts = getProxyParts(saved.proxyUrl ?? null);
      setProxyUrl(parts.host);
      setProxyUsername(parts.username);
      setProxyPassword(parts.password);
      setKeepExistingPassword(parts.hasStoredPassword && !parts.password);
      setNoProxy(saved.noProxy ?? "");
      toast.success(t(($) => $.desktop.network.proxy_saved), {
        id: "proxy-saved",
      });
    } catch {
      toast.error(t(($) => $.desktop.network.proxy_save_failed));
    } finally {
      setSaving(false);
    }
  }, [keepExistingPassword, noProxy, proxyPassword, proxyUrl, proxyUsername, t]);

  const handleTest = useCallback(async () => {
    const trimmed = buildProxyUrl(proxyUrl, proxyUsername, proxyPassword, keepExistingPassword).trim();
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
  }, [keepExistingPassword, proxyPassword, proxyUrl, proxyUsername]);

  return (
    <SettingsTab title={t(($) => $.desktop.network.title)}>
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.network.proxy_title)}
          description={t(($) => $.desktop.network.proxy_description)}
        >
          <div className="flex w-full flex-col gap-2">
            <Input
              type="text"
              autoComplete="off"
              aria-label={t(($) => $.desktop.network.proxy_title)}
              value={proxyUrl}
              onChange={(e) => setProxyUrl(e.target.value)}
              placeholder={t(($) => $.desktop.network.proxy_placeholder)}
              disabled={saving}
            />
            <div className="flex gap-2">
              <Input
                type="text"
                autoComplete="username"
                aria-label="Proxy username"
                value={proxyUsername}
                onChange={(e) => setProxyUsername(e.target.value)}
                placeholder="Username"
                disabled={saving}
              />
              <Input
                type="password"
                autoComplete="current-password"
                aria-label="Proxy password"
                value={proxyPassword}
                onChange={(e) => {
                  setProxyPassword(e.target.value);
                  setKeepExistingPassword(false);
                }}
                placeholder="Password"
                disabled={saving}
              />
            </div>
          </div>
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.network.no_proxy_title)}
          description={t(($) => $.desktop.network.no_proxy_description)}
        >
          <Input
            value={noProxy}
            onChange={(e) => setNoProxy(e.target.value)}
            placeholder={t(($) => $.desktop.network.no_proxy_placeholder)}
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
            disabled={!buildProxyUrl(proxyUrl, proxyUsername, proxyPassword, keepExistingPassword).trim() || testState.status === "testing"}
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

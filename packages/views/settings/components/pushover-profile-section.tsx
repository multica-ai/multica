"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
} from "./settings-layout";
import { useAutoSave } from "./use-auto-save";

const PUSHOVER_KEY_PATTERN = /^[A-Za-z0-9]{30}$/;

type PushoverAction = "disconnecting" | "testing" | "updating" | null;

export function PushoverProfileSection() {
  const { t } = useT("settings");
  const available = useConfigStore((state) => state.pushoverAvailable);
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [userKey, setUserKey] = useState("");
  const [loginCodesEnabled, setLoginCodesEnabled] = useState(
    user?.pushover_login_codes_enabled === true,
  );
  const [action, setAction] = useState<PushoverAction>(null);

  const hasStoredKey = user?.pushover_user_key_configured === true;
  const trimmedUserKey = userKey.trim();

  useEffect(() => {
    setUserKey("");
    setLoginCodesEnabled(user?.pushover_login_codes_enabled === true);
  }, [user?.id, user?.pushover_login_codes_enabled, user?.pushover_user_key_configured]);

  const connectPushover = useCallback(
    async (nextKey: string) => {
      const updated = await api.updatePushoverSettings({
        user_key: nextKey.trim(),
        login_codes_enabled: true,
      });
      setUser(updated);
      setUserKey("");
      setLoginCodesEnabled(true);
    },
    [setUser],
  );

  const keyAutoSave = useAutoSave({
    value: userKey,
    savedValue: "",
    onSave: connectPushover,
    onSuccess: () => toast.success(t(($) => $.pushover.profile.connected)),
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : t(($) => $.pushover.profile.connect_failed),
      ),
    enabled: available && !hasStoredKey && PUSHOVER_KEY_PATTERN.test(trimmedUserKey),
    isEqual: (left, right) => left.trim() === right.trim(),
  });

  if (!available) return null;

  async function disconnect() {
    if (action) return;
    setAction("disconnecting");
    try {
      const updated = await api.updatePushoverSettings({
        user_key: "",
        login_codes_enabled: false,
      });
      setUser(updated);
      setUserKey("");
      setLoginCodesEnabled(false);
      toast.success(t(($) => $.pushover.profile.disconnected));
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.pushover.profile.disconnect_failed),
      );
    } finally {
      setAction(null);
    }
  }

  async function sendTestNotification() {
    if (action) return;
    setAction("testing");
    try {
      await api.sendPushoverTestNotification();
      toast.success(t(($) => $.pushover.profile.test_sent));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.pushover.profile.test_failed),
      );
    } finally {
      setAction(null);
    }
  }

  async function updateLoginCodes(enabled: boolean) {
    if (action || !hasStoredKey) return;
    const previous = loginCodesEnabled;
    setLoginCodesEnabled(enabled);
    setAction("updating");
    try {
      const updated = await api.updatePushoverSettings({ login_codes_enabled: enabled });
      setUser(updated);
    } catch (error) {
      setLoginCodesEnabled(previous);
      toast.error(
        error instanceof Error ? error.message : t(($) => $.pushover.profile.save_failed),
      );
    } finally {
      setAction(null);
    }
  }

  return (
    <SettingsSection
      title={t(($) => $.pushover.profile.section_title)}
      description={t(($) => $.pushover.profile.section_description)}
      action={
        !hasStoredKey ? (
          <SettingsSaveState
            status={keyAutoSave.status}
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        ) : undefined
      }
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.pushover.profile.user_key_label)}
          description={
            <span>
              {t(($) => $.pushover.profile.user_key_description_before)}
              <a
                className="inline-flex items-center gap-1 text-primary hover:underline"
                href="https://pushover.net/"
                target="_blank"
                rel="noreferrer"
              >
                {t(($) => $.pushover.profile.dashboard_link)}
                <ExternalLink className="size-3" />
              </a>
              {t(($) => $.pushover.profile.user_key_description_after)}
            </span>
          }
          size={hasStoredKey ? "none" : "text"}
          align="start"
        >
          {hasStoredKey ? (
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={disconnect}
                disabled={action !== null}
              >
                {t(($) => $.pushover.profile.disconnect)}
              </Button>
              <Button
                size="sm"
                onClick={sendTestNotification}
                disabled={action !== null}
              >
                {t(($) => $.pushover.profile.send_test)}
              </Button>
            </div>
          ) : (
            <Input
              type="password"
              name="pushover-user-key"
              autoComplete="off"
              aria-label={t(($) => $.pushover.profile.user_key_label)}
              value={userKey}
              onChange={(event) => setUserKey(event.target.value)}
              onBlur={keyAutoSave.flush}
              placeholder={t(($) => $.pushover.profile.user_key_placeholder)}
              maxLength={30}
            />
          )}
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.pushover.profile.login_codes_label)}
          description={t(($) => $.pushover.profile.login_codes_description)}
        >
          <Switch
            checked={loginCodesEnabled}
            aria-label={t(($) => $.pushover.profile.login_codes_label)}
            onCheckedChange={updateLoginCodes}
            disabled={action !== null || !hasStoredKey}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

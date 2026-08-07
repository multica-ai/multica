"use client";

import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

export function PushoverProfileSection() {
  const { t } = useT("settings");
  const available = useConfigStore((state) => state.pushoverAvailable);
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [userKey, setUserKey] = useState("");
  const [loginCodesEnabled, setLoginCodesEnabled] = useState(
    user?.pushover_login_codes_enabled === true,
  );
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setUserKey("");
    setLoginCodesEnabled(user?.pushover_login_codes_enabled === true);
  }, [user?.id, user?.pushover_login_codes_enabled]);

  if (!available) return null;

  const hasStoredKey = user?.pushover_user_key_configured === true;
  const hasUsableKey = hasStoredKey || userKey.trim().length > 0;

  async function save() {
    if (saving || (loginCodesEnabled && !hasUsableKey)) return;
    setSaving(true);
    try {
      const trimmedKey = userKey.trim();
      const updated = await api.updatePushoverSettings({
        ...(trimmedKey ? { user_key: trimmedKey } : {}),
        login_codes_enabled: loginCodesEnabled,
      });
      setUser(updated);
      setUserKey("");
      toast.success(t(($) => $.pushover.profile.saved));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.pushover.profile.save_failed),
      );
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (saving) return;
    setSaving(true);
    try {
      const updated = await api.updatePushoverSettings({
        user_key: "",
        login_codes_enabled: false,
      });
      setUser(updated);
      setUserKey("");
      setLoginCodesEnabled(false);
      toast.success(t(($) => $.pushover.profile.removed));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.pushover.profile.remove_failed),
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <SettingsSection
      title={t(($) => $.pushover.profile.section_title)}
      description={t(($) => $.pushover.profile.section_description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.pushover.profile.user_key_label)}
          description={
            <span>
              {t(($) => $.pushover.profile.user_key_description)}{" "}
              <a
                className="inline-flex items-center gap-1 text-primary hover:underline"
                href="https://pushover.net/"
                target="_blank"
                rel="noreferrer"
              >
                {t(($) => $.pushover.profile.dashboard_link)}
                <ExternalLink className="size-3" />
              </a>
            </span>
          }
          size="text"
          align="start"
        >
          <div className="space-y-2">
            <Input
              type="password"
              name="pushover-user-key"
              autoComplete="off"
              aria-label={t(($) => $.pushover.profile.user_key_label)}
              value={userKey}
              onChange={(event) => setUserKey(event.target.value)}
              placeholder={
                hasStoredKey
                  ? t(($) => $.pushover.profile.user_key_configured)
                  : t(($) => $.pushover.profile.user_key_placeholder)
              }
              disabled={saving}
            />
            <div className="flex flex-wrap justify-end gap-2">
              {hasStoredKey ? (
                <Button variant="outline" size="sm" onClick={remove} disabled={saving}>
                  {t(($) => $.pushover.profile.remove_key)}
                </Button>
              ) : null}
              <Button
                size="sm"
                onClick={save}
                disabled={saving || (loginCodesEnabled && !hasUsableKey)}
              >
                {saving ? t(($) => $.pushover.profile.saving) : t(($) => $.pushover.profile.save)}
              </Button>
            </div>
          </div>
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.pushover.profile.login_codes_label)}
          description={t(($) => $.pushover.profile.login_codes_description)}
        >
          <Switch
            checked={loginCodesEnabled}
            aria-label={t(($) => $.pushover.profile.login_codes_label)}
            onCheckedChange={setLoginCodesEnabled}
            disabled={saving || !hasUsableKey}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

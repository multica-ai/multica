import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { toast } from "sonner";
import {
  isCloseBehavior,
  type CloseBehavior,
} from "../../../shared/desktop-preferences";

export function DesktopSettingsTab() {
  const { t } = useT("settings");
  const [closeBehavior, setCloseBehavior] = useState<CloseBehavior>("tray");
  const [ready, setReady] = useState(false);
  const [saving, setSaving] = useState(false);
  const items = useMemo(
    () => [
      {
        value: "tray" as const,
        label: t(($) => $.desktop.app.close_behavior_tray),
      },
      {
        value: "taskbar" as const,
        label: t(($) => $.desktop.app.close_behavior_taskbar),
      },
      {
        value: "quit" as const,
        label: t(($) => $.desktop.app.close_behavior_quit),
      },
    ],
    [t],
  );

  useEffect(() => {
    let mounted = true;
    void window.desktopPreferences
      .get()
      .then((preferences) => {
        if (mounted) setCloseBehavior(preferences.closeBehavior);
      })
      .catch(() => {
        // Main owns the default. Keep close-to-tray if IPC is unavailable.
      })
      .finally(() => {
        if (mounted) setReady(true);
      });
    return () => {
      mounted = false;
    };
  }, []);

  const updateCloseBehavior = useCallback(
    async (value: string | null) => {
      if (!isCloseBehavior(value)) return;
      setSaving(true);
      try {
        const preferences =
          await window.desktopPreferences.setCloseBehavior(value);
        setCloseBehavior(preferences.closeBehavior);
        toast.success(t(($) => $.auto_save.toast_saved), {
          id: "settings-auto-save",
        });
      } catch {
        toast.error(t(($) => $.desktop.app.close_behavior_save_failed));
      } finally {
        setSaving(false);
      }
    },
    [t],
  );

  const selectedLabel =
    items.find((item) => item.value === closeBehavior)?.label ?? items[0].label;

  return (
    <SettingsTab
      title={t(($) => $.desktop.app.title)}
      description={t(($) => $.desktop.app.description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.app.close_behavior_title)}
          description={t(($) => $.desktop.app.close_behavior_description)}
        >
          <Select
            items={items}
            value={closeBehavior}
            disabled={!ready || saving}
            onValueChange={updateCloseBehavior}
          >
            <SelectTrigger
              size="sm"
              className="w-56"
              aria-label={t(($) => $.desktop.app.close_behavior_title)}
            >
              <SelectValue>{selectedLabel}</SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingsRow>
      </SettingsCard>
    </SettingsTab>
  );
}

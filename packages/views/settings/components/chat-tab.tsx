"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import { useChatStore } from "@multica/core/chat";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Chat settings — its own tab under "My Account". Currently just the
 * floating-window toggle: when off, the FAB / overlay never mount and Chat
 * is reachable only from its dedicated tab. It is on by default. The preference is
 * a persisted client setting (`floatingChatEnabled`), so it applies
 * immediately without a round-trip.
 */
export function ChatTab() {
  const { t } = useT("settings");
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const setEnabled = useChatStore((s) => s.setFloatingChatEnabled);

  return (
    <SettingsTab title={t(($) => $.page.tabs.chat)}>
      <SettingsSection title={t(($) => $.chat.floating_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.chat.floating_label)}
            description={t(($) => $.chat.floating_hint)}
          >
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            aria-label={t(($) => $.chat.floating_label)}
          />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

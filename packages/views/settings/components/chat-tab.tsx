"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import { useChatStore } from "@multica/core/chat";
import { useT } from "../../i18n";

/**
 * Chat settings — its own tab under "My Account". Currently just the
 * floating-window toggle: when off (the default), the FAB / overlay never
 * mount and Chat is reachable only from its dedicated tab. The preference is
 * a persisted client setting (`floatingChatEnabled`), so it applies
 * immediately without a round-trip.
 */
export function ChatTab() {
  const { t } = useT("settings");
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const setEnabled = useChatStore((s) => s.setFloatingChatEnabled);

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.chat.floating_title)}</h2>
        <label className="flex items-center justify-between gap-4">
          <div className="space-y-0.5 pr-4">
            <p className="text-sm font-medium">{t(($) => $.chat.floating_label)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.chat.floating_hint)}
            </p>
          </div>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </label>
      </section>
    </div>
  );
}

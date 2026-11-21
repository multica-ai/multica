import { Switch } from "@multica/ui/components/ui/switch";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
} from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import {
  setRunIslandEnabled,
  useRunIslandEnabled,
} from "../platform/run-island-bridge";

export function RunIslandPreference() {
  const { t } = useT("settings");
  const enabled = useRunIslandEnabled();
  const title = t(($) => $.desktop.island.title);
  return (
    <SettingsSection title={title} scope="device">
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.desktop.island.show)}
          description={t(($) => $.desktop.island.description)}
        >
          <Switch
            aria-label={title}
            checked={enabled}
            onCheckedChange={setRunIslandEnabled}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

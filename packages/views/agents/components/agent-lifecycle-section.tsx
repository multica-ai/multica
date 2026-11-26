"use client";

import { Archive, ArchiveRestore } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useT } from "../../i18n";

/**
 * Archive / Restore at the foot of Settings → General — where people look for
 * an agent's lifecycle. Same actions as the page's More menu.
 */
export function AgentLifecycleSection({
  archived,
  onArchive,
  onRestore,
}: {
  archived: boolean;
  onArchive: () => void;
  onRestore: () => void;
}) {
  const { t } = useT("agents");

  if (archived) {
    return (
      <SettingsSection title={t(($) => $.row.archived)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.lifecycle.restore_label)}
            description={t(($) => $.detail.archived_banner)}
          >
            <Button variant="outline" size="sm" onClick={onRestore}>
              <ArchiveRestore />
              {t(($) => $.row_actions.restore)}
            </Button>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    );
  }

  return (
    <SettingsSection title={t(($) => $.lifecycle.danger_zone)}>
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.detail.more_archive)}
          description={t(($) => $.row_actions.archive_dialog_description)}
        >
          <Button variant="outline" size="sm" onClick={onArchive}>
            <Archive />
            {t(($) => $.row_actions.archive)}
          </Button>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

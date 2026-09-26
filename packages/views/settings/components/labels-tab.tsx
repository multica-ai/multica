"use client";

import { useT } from "../../i18n";
import { LabelManager } from "../../labels/label-manager";
import { SettingsTab } from "./settings-layout";

/** Issue labels. Skill labels are managed on the Skills page. */
export function LabelsTab() {
  const { t } = useT("settings");
  return (
    <SettingsTab
      title={t(($) => $.labels.title)}
      description={t(($) => $.labels.description)}
      scope="workspace"
    >
      <LabelManager scope="issue" />
    </SettingsTab>
  );
}

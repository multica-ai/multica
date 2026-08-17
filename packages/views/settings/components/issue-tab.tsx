"use client";

import { Switch } from "@multica/ui/components/ui/switch";
import {
  MANUAL_CREATE_FIELDS,
  QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
} from "@multica/core/issues/stores/issue-create-settings-store";
import { toast } from "sonner";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Issue settings — its own tab under "My Account". One group per create-issue
 * mode (agent quick create / manual create), each a switch list of the fields
 * that mode keeps on its dialog toolbar. Persisted client-side per workspace;
 * a field toggled off stays reachable from the dialog's ⋯ overflow and
 * re-surfaces automatically while it holds a value, so hiding is never
 * destructive.
 */
export function IssueTab() {
  const { t } = useT("settings");
  const quickFields = useIssueCreateSettingsStore((s) => s.quickCreateFields);
  const setQuickVisible = useIssueCreateSettingsStore((s) => s.setQuickCreateFieldVisible);
  const manualFields = useIssueCreateSettingsStore((s) => s.manualCreateFields);
  const setManualVisible = useIssueCreateSettingsStore((s) => s.setManualCreateFieldVisible);

  const savedToast = () =>
    toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.issue)}
      description={t(($) => $.issue.description)}
    >
      <SettingsSection
        title={t(($) => $.issue.quick_create_title)}
        description={t(($) => $.issue.quick_create_description)}
      >
        <SettingsCard>
          {QUICK_CREATE_FIELDS.map((field) => (
            <SettingsRow key={field} label={t(($) => $.issue.fields[field])}>
              <Switch
                checked={quickFields.includes(field)}
                onCheckedChange={(checked) => {
                  setQuickVisible(field, checked);
                  savedToast();
                }}
                aria-label={t(($) => $.issue.fields[field])}
              />
            </SettingsRow>
          ))}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.issue.manual_create_title)}
        description={t(($) => $.issue.manual_create_description)}
      >
        <SettingsCard>
          {MANUAL_CREATE_FIELDS.map((field) => (
            <SettingsRow key={field} label={t(($) => $.issue.fields[field])}>
              <Switch
                checked={manualFields.includes(field)}
                onCheckedChange={(checked) => {
                  setManualVisible(field, checked);
                  savedToast();
                }}
                aria-label={t(($) => $.issue.fields[field])}
              />
            </SettingsRow>
          ))}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

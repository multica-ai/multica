"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { propertyListOptions } from "@multica/core/properties";
import {
  MANUAL_CREATE_FIELDS,
  QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
} from "@multica/core/issues/stores/issue-create-settings-store";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";
import { PropertyIcon } from "../../common/property-icon";
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from "./settings-layout";

export function IssueTab() {
  const { t } = useT("settings");
  const workspaceId = useWorkspaceId();
  const { data: properties = [] } = useQuery(propertyListOptions(workspaceId));
  const quickFields = useIssueCreateSettingsStore((state) => state.quickCreateFields);
  const setQuickVisible = useIssueCreateSettingsStore((state) => state.setQuickCreateFieldVisible);
  const manualFields = useIssueCreateSettingsStore((state) => state.manualCreateFields);
  const setManualVisible = useIssueCreateSettingsStore((state) => state.setManualCreateFieldVisible);
  const hiddenPropertyIds = useIssueCreateSettingsStore((state) => state.hiddenManualPropertyIds);
  const setPropertyVisible = useIssueCreateSettingsStore((state) => state.setManualPropertyVisible);
  const activeProperties = properties.filter((property) => !property.archived);
  const saved = () => toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });

  return (
    <SettingsTab title={t(($) => $.page.tabs.issue)} description={t(($) => $.issue.description)}>
      <SettingsSection
        title={t(($) => $.issue.quick_create_title)}
        description={t(($) => $.issue.quick_create_description)}
      >
        <SettingsCard>
          {QUICK_CREATE_FIELDS.map((field) => (
            <SettingsRow key={field} label={t(($) => $.issue.fields[field])}>
              <Switch
                checked={quickFields.includes(field)}
                onCheckedChange={(checked) => { setQuickVisible(field, checked); saved(); }}
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
                onCheckedChange={(checked) => { setManualVisible(field, checked); saved(); }}
                aria-label={t(($) => $.issue.fields[field])}
              />
            </SettingsRow>
          ))}
        </SettingsCard>
      </SettingsSection>

      {activeProperties.length > 0 && (
        <SettingsSection
          title={t(($) => $.issue.custom_properties_title)}
          description={t(($) => $.issue.custom_properties_description)}
        >
          <SettingsCard>
            {activeProperties.map((property) => (
              <SettingsRow
                key={property.id}
                label={<span className="flex items-center gap-2"><PropertyIcon property={property} />{property.name}</span>}
              >
                <Switch
                  checked={!hiddenPropertyIds.includes(property.id)}
                  onCheckedChange={(checked) => { setPropertyVisible(property.id, checked); saved(); }}
                  aria-label={property.name}
                />
              </SettingsRow>
            ))}
          </SettingsCard>
        </SettingsSection>
      )}
    </SettingsTab>
  );
}

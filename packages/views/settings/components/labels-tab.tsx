"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { labelListOptions } from "@multica/core/labels";
import { useT } from "../../i18n";
import { LabelManager, type LabelScope } from "../../labels/label-manager";
import { useNavigation } from "../../navigation";
import { SettingsTab, SettingsViewTabs } from "./settings-layout";

/**
 * The workspace's label catalogs. Issue and skill labels are separate lists;
 * agent labels were removed from the product (MUL-5600) even though the
 * backend still models the `agent` resource type.
 *
 * The open catalog lives in `?scope=`, so a label picker's "Manage labels…"
 * can land directly on the list it picks from.
 */
export function LabelsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const scope: LabelScope =
    navigation.searchParams.get("scope") === "skill" ? "skill" : "issue";
  const setScope = (next: LabelScope) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (next === "skill") params.set("scope", next);
    else params.delete("scope");
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
  };
  // Same queries the manager runs, so the counts come from the shared cache.
  const { data: issueLabels } = useQuery(labelListOptions(wsId, "issue"));
  const { data: skillLabels } = useQuery(labelListOptions(wsId, "skill"));

  return (
    <SettingsTab
      title={t(($) => $.labels.title)}
      description={t(($) => $.labels.description)}
      scope="workspace"
    >
      <div className="space-y-4">
        <SettingsViewTabs
          label={t(($) => $.labels.title)}
          items={[
            { value: "issue", label: t(($) => $.labels.scopes.issue), count: issueLabels?.length },
            { value: "skill", label: t(($) => $.labels.scopes.skill), count: skillLabels?.length },
          ]}
          value={scope}
          onChange={setScope}
        />
        {/* Keyed so switching lists also clears the search. */}
        <LabelManager key={scope} scope={scope} />
      </div>
    </SettingsTab>
  );
}

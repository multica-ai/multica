import { agentCapabilitiesSettingsTab } from "@multica/cerebro-agent-capabilities";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags/settings-tabs";
import { DocsPanel } from "./docs-panel";
import { SettingsPageClient } from "./settings-page-client";

export default function Page() {
  return (
    <SettingsPageClient
      extraAccountTabs={[agentCapabilitiesSettingsTab, ...cerebroFeatureFlagTabs]}
      documentationContent={<DocsPanel />}
    />
  );
}

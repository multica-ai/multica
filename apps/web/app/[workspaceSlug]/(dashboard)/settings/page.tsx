import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags";
import { DocsPanel } from "./docs-panel";
import { SettingsPageClient } from "./settings-page-client";

export default function Page() {
  return (
    <SettingsPageClient
      extraAccountTabs={cerebroFeatureFlagTabs}
      documentationContent={<DocsPanel />}
    />
  );
}

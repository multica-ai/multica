import { SettingsPage } from "@multica/views/settings";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags";
import { DocsPanel } from "./docs-panel";

export default function Page() {
  return (
    <SettingsPage
      extraAccountTabs={cerebroFeatureFlagTabs}
      documentationContent={<DocsPanel />}
    />
  );
}

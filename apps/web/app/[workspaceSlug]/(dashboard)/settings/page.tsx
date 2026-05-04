import { SettingsPage } from "@multica/views/settings";
import { DocsPanel } from "./docs-panel";

export default function Page() {
  return <SettingsPage documentationContent={<DocsPanel />} />;
}

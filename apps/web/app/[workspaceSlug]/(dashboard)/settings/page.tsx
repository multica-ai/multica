import { DocsPanel } from "./docs-panel";
import { SettingsPageClient } from "./settings-page-client";

export default function Page() {
  // The extra account tabs (agent capabilities, cerebro feature flags) carry
  // lucide icon *components* in their `icon` field. Component functions cannot
  // be serialized across the Server -> Client Component boundary, so the tab
  // descriptors are assembled inside SettingsPageClient ("use client") instead
  // of being passed down from here. DocsPanel must stay server-rendered (it
  // uses fumadocs' server-only source API), so it is passed as an already
  // rendered element child, which RSC allows.
  return <SettingsPageClient documentationContent={<DocsPanel />} />;
}

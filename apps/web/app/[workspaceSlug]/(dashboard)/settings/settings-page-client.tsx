"use client";

import type { ReactNode } from "react";
import { SettingsPage, type ExtraSettingsTab } from "@multica/views/settings";
import { useMembersTabCerebroExtras } from "@multica/cerebro-members/views";

export function SettingsPageClient({
  extraAccountTabs,
  documentationContent,
}: {
  extraAccountTabs?: ExtraSettingsTab[];
  documentationContent?: ReactNode;
}) {
  const membersTabCerebroExtras = useMembersTabCerebroExtras();

  return (
    <SettingsPage
      extraAccountTabs={extraAccountTabs}
      membersTabCerebroExtras={membersTabCerebroExtras}
      documentationContent={documentationContent}
    />
  );
}

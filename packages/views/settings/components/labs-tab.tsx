"use client";

import { FlaskConical } from "lucide-react";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { useT } from "../../i18n";

// The Co-authored-by trailer toggle moved into the dedicated GitHub Settings
// tab (see github-tab.tsx). Labs is kept as a container for future
// experimental flags rather than removed from the IA.
export function LabsTab() {
  const { t } = useT("settings");
  return (
    <div className="space-y-4">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FlaskConical className="h-4 w-4" />
          </EmptyMedia>
          <EmptyTitle>{t(($) => $.labs.section_placeholder_title)}</EmptyTitle>
          <EmptyDescription>
            {t(($) => $.labs.section_placeholder_description)}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    </div>
  );
}

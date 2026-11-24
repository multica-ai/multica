"use client";

import { Check } from "@multica/ui/icons";
import { useT } from "../../i18n";
import { ObjectPanelSection } from "../../layout/object-page";

const CLASSES = ["read", "write", "send", "delete", "share"] as const;

/** The kinds of action an agent with this app can take. */
export function AppCapabilitiesCard() {
  const { t } = useT("settings");
  return (
    <ObjectPanelSection title={t(($) => $.connected_apps.app_page.capabilities_title)}>
      <ul className="space-y-1.5">
        {CLASSES.map((cls) => (
          <li key={cls} className="flex items-start gap-2 text-caption">
            <Check aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-success" />
            <span>
              <span className="font-medium">{t(($) => $.connected_apps.classes[cls].label)}</span>
              <span className="text-muted-foreground"> · {t(($) => $.connected_apps.classes[cls].description)}</span>
            </span>
          </li>
        ))}
      </ul>
      <p className="pt-2 text-caption text-muted-foreground">
        {t(($) => $.connected_apps.app_page.capabilities_note)}
      </p>
    </ObjectPanelSection>
  );
}

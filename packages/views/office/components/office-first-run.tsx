"use client";

import { Building2 } from "@multica/ui/icons";
import { useModalStore } from "@multica/core/modals";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Shown in place of an empty floor while no squad is seated anywhere: the two
 * ways to put the first squad in a room.
 */
export function OfficeFirstRun() {
  const { t } = useT("issues");
  const open = useModalStore.getState().open;
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center p-6">
      <section className="flex max-w-sm flex-col items-center gap-4 rounded-xl border bg-card p-6 text-center">
        <Building2 className="size-8 text-muted-foreground" />
        <h2 className="text-heading">{t(($) => $.office.first_run_title)}</h2>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button type="button" onClick={() => open("squad-quickstart")}>
            {t(($) => $.office.first_run_template)}
          </Button>
          <Button type="button" variant="outline" onClick={() => open("create-squad")}>
            {t(($) => $.office.create_squad)}
          </Button>
        </div>
      </section>
    </div>
  );
}

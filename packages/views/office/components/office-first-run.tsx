"use client";

import { useState } from "react";
import { Building2 } from "@multica/ui/icons";
import { useModalStore } from "@multica/core/modals";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { useOfficeAdmin } from "../lib/use-office-admin";
import { OfficeCompanyWizard } from "./office-company-wizard";

/**
 * Shown in place of an empty floor while no squad is seated anywhere: the ways
 * to put the first squads in a room. Owners and admins can build the starter
 * company; everyone can still start one squad from a template or by hand.
 */
export function OfficeFirstRun() {
  const { t } = useT("issues");
  const canManage = useOfficeAdmin();
  const [companyOpen, setCompanyOpen] = useState(false);
  const open = useModalStore.getState().open;
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center p-6">
      <section className="flex max-w-sm flex-col items-center gap-4 rounded-xl border bg-card p-6 text-center">
        <Building2 className="size-8 text-muted-foreground" />
        <h2 className="text-heading">{t(($) => $.office.first_run_title)}</h2>
        <div className="flex flex-wrap items-center justify-center gap-2">
          {canManage ? (
            <Button type="button" onClick={() => setCompanyOpen(true)}>
              {t(($) => $.office.company_entry)}
            </Button>
          ) : null}
          <Button
            type="button"
            variant={canManage ? "outline" : "default"}
            onClick={() => open("squad-quickstart")}
          >
            {t(($) => $.office.first_run_template)}
          </Button>
          <Button type="button" variant="outline" onClick={() => open("create-squad")}>
            {t(($) => $.office.create_squad)}
          </Button>
        </div>
      </section>
      {companyOpen ? (
        <OfficeCompanyWizard open onClose={() => setCompanyOpen(false)} />
      ) : null}
    </div>
  );
}

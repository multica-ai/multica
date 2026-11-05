"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { IssuePeekHost } from "../../issues/components/issue-peek";
import { PageHeader } from "../../layout/page-header";
import { CompanyFloorPlan } from "./company-plan";
import { CompanyHeaderSlotContext } from "./company-header-slot";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import {
  companySelectionQuery,
  readCompanySelection,
  type CompanyView,
} from "../lib/company-selection";
import { CompanyOverview } from "./company-overview";

/**
 * The company's one page: the floor plan by default, and the org chart to drill
 * through the departments, squads and agents of the same company.
 */
export function CompanyPage() {
  const { t } = useT("company");
  const { pathname, searchParams, push } = useNavigation();
  const selection = readCompanySelection(searchParams);
  const workspace = useCurrentWorkspace();
  const [headerSlot, setHeaderSlot] = useState<HTMLDivElement | null>(null);

  const setView = (view: CompanyView) =>
    push(`${pathname}${companySelectionQuery({ ...selection, view })}`);

  return (
    <IssuePeekHost>
      <CompanyHeaderSlotContext.Provider value={headerSlot}>
        <div className="flex min-h-0 flex-1 flex-col">
          <PageHeader>
            <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
            {workspace ? <span className="truncate text-sm text-muted-foreground">{workspace.name}</span> : null}
            <div ref={setHeaderSlot} className="flex min-w-0 flex-1 items-center" />
            <Tabs
              value={selection.view}
              onValueChange={(value) => setView(value as CompanyView)}
            >
              <TabsList>
                <TabsTrigger value="plan">{t(($) => $.tab_plan)}</TabsTrigger>
                <TabsTrigger value="overview">{t(($) => $.tab_org_chart)}</TabsTrigger>
              </TabsList>
            </Tabs>
          </PageHeader>
          {selection.view === "overview" ? <CompanyOverview selection={selection} /> : <CompanyFloorPlan />}
        </div>
      </CompanyHeaderSlotContext.Provider>
    </IssuePeekHost>
  );
}

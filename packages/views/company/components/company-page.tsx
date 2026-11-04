"use client";

import { useCurrentWorkspace } from "@multica/core/paths";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { IssuePeekHost } from "../../issues/components/issue-peek";
import { PageHeader } from "../../layout/page-header";
import { CompanyFloorPlan } from "./company-plan";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import {
  companySelectionQuery,
  readCompanySelection,
  type CompanyView,
} from "../lib/company-selection";
import { CompanyOverview } from "./company-overview";

/**
 * The company's one page: the overview to drill through the departments,
 * squads and agents, and the floor plan of the same company.
 */
export function CompanyPage() {
  const { t } = useT("company");
  const { pathname, searchParams, push } = useNavigation();
  const selection = readCompanySelection(searchParams);
  const workspace = useCurrentWorkspace();

  const setView = (view: CompanyView) =>
    push(`${pathname}${companySelectionQuery({ ...selection, view })}`);

  return (
    <IssuePeekHost>
      <div className="flex min-h-0 flex-1 flex-col">
        <PageHeader>
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
          {workspace ? <span className="truncate text-sm text-muted-foreground">{workspace.name}</span> : null}
          <Tabs
            value={selection.view}
            onValueChange={(value) => setView(value as CompanyView)}
            className="ml-auto"
          >
            <TabsList>
              <TabsTrigger value="overview">{t(($) => $.tab_overview)}</TabsTrigger>
              <TabsTrigger value="plan">{t(($) => $.tab_plan)}</TabsTrigger>
            </TabsList>
          </Tabs>
        </PageHeader>
        {selection.view === "plan" ? <CompanyFloorPlan /> : <CompanyOverview selection={selection} />}
      </div>
    </IssuePeekHost>
  );
}

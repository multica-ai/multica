"use client";

import { parseGovernanceTab, type GovernanceTab } from "@multica/core/paths";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Shield } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageHeader } from "../layout/collection-page";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { GovernanceAutonomyTab } from "./governance-autonomy-tab";
import { GovernanceBudgetsTab } from "./governance-budgets-tab";
import { GovernanceControlsTab } from "./governance-controls-tab";
import { GovernanceGoalsTab } from "./governance-goals-tab";
import { GovernanceLogTab } from "./governance-log-tab";
import { GovernanceReviewsTab } from "./governance-reviews-tab";

/**
 * Governance: how the company is steered. Six tabs, selected by the URL's
 * `tab` query so a tab can be linked and survives a reload. Goals is the
 * default. The tab bodies that are not built yet render their empty state.
 */
export function GovernancePage() {
  const { t } = useT("governance");
  const { pathname, searchParams, replace } = useNavigation();
  const tab = parseGovernanceTab(searchParams.get("tab"));

  const selectTab = (next: GovernanceTab) => {
    const params = new URLSearchParams(searchParams);
    params.set("tab", next);
    replace(`${pathname}?${params.toString()}`);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader icon={Shield} title={t(($) => $.title)} />
      <Tabs
        value={tab}
        onValueChange={(value) => selectTab(parseGovernanceTab(String(value)))}
        className="min-h-0 flex-1 gap-0"
      >
        <div className={cn(PAGE_GUTTER, "pt-2")}>
          <TabsList variant="line" className="flex-wrap">
            <TabsTrigger value="goals">{t(($) => $.tabs.goals)}</TabsTrigger>
            <TabsTrigger value="budgets">{t(($) => $.tabs.budgets)}</TabsTrigger>
            <TabsTrigger value="autonomy">{t(($) => $.tabs.autonomy)}</TabsTrigger>
            <TabsTrigger value="reviews">{t(($) => $.tabs.reviews)}</TabsTrigger>
            <TabsTrigger value="log">{t(($) => $.tabs.log)}</TabsTrigger>
            <TabsTrigger value="controls">{t(($) => $.tabs.controls)}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="goals" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceGoalsTab />
        </TabsContent>
        <TabsContent value="budgets" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceBudgetsTab />
        </TabsContent>
        <TabsContent value="autonomy" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceAutonomyTab />
        </TabsContent>
        <TabsContent value="reviews" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceReviewsTab />
        </TabsContent>
        <TabsContent value="log" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceLogTab />
        </TabsContent>
        <TabsContent value="controls" className="min-h-0 flex-1 overflow-y-auto">
          <GovernanceControlsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

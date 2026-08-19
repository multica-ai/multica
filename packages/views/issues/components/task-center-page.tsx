"use client";

import { taskCenterPath, taskCenterTabFromSearch } from "@multica/core/issues/task-center";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { MyIssuesPage } from "../../my-issues/components/my-issues-page";
import { InboxPage } from "../../inbox/components/inbox-page";
import { AutopilotsPage } from "../../autopilots/components/autopilots-page";
import { IssuesPage } from "./issues-page";

const TABS = ["tasks", "mine", "activity", "automations"] as const;

export function TaskCenterPage({
  workspaceSlug,
  automationDetailHref,
}: {
  workspaceSlug: string;
  automationDetailHref?: (autopilotId: string) => string;
}) {
  const { searchParams, replace } = useNavigation();
  const { t } = useT("issues");
  const activeTab = taskCenterTabFromSearch(searchParams);
  const labels = {
    tasks: t(($) => $.task_center.tasks),
    mine: t(($) => $.task_center.mine),
    activity: t(($) => $.task_center.activity),
    automations: t(($) => $.task_center.automations),
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <nav
        role="tablist"
        aria-label={t(($) => $.task_center.navigation_label)}
        className="flex h-12 shrink-0 items-end gap-1 border-b px-4"
      >
        {TABS.map((tab) => (
          <button
            key={tab}
            type="button"
            role="tab"
            aria-selected={activeTab === tab}
            className={cn(
              "h-full border-b-2 px-2.5 text-caption font-medium transition-colors",
              activeTab === tab
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
            onClick={() => replace(taskCenterPath(workspaceSlug, tab))}
          >
            {labels[tab]}
          </button>
        ))}
      </nav>

      {activeTab === "tasks" ? <IssuesPage /> : null}
      {activeTab === "mine" ? <MyIssuesPage title={labels.mine} /> : null}
      {activeTab === "activity" ? (
        <InboxPage title={labels.activity} backLabel={labels.activity} />
      ) : null}
      {activeTab === "automations" ? (
        <AutopilotsPage
          title={labels.automations}
          createLabel={t(($) => $.task_center.new_automation)}
          emptyTitle={t(($) => $.task_center.no_automations)}
          emptyHint={t(($) => $.task_center.automations_hint)}
          detailHref={automationDetailHref}
        />
      ) : null}
    </div>
  );
}

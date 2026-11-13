import { useState, type ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import type { ScheduleConfig } from "@multica/core/autopilots/schedule";
import { TabBar } from "../ui/components/TabBar";
import { tabItems } from "../ui/navigation/tabs";
import { AutopilotEditorView, type AutopilotDraft, type AutopilotEditorPage } from "../ui/screens/autopilots/AutopilotEditorView";
import { AutopilotsView } from "../ui/screens/autopilots/AutopilotsView";
import { AutopilotView } from "../ui/screens/autopilots/AutopilotView";
import { TriggerEditorView } from "../ui/screens/autopilots/TriggerEditorView";
import { ProjectEditorView, type ProjectEditorPage } from "../ui/screens/projects/ProjectEditorView";
import { ProjectsView } from "../ui/screens/projects/ProjectsView";
import { ProjectView } from "../ui/screens/projects/ProjectView";
import { SquadEditorView, type SquadEditorPage } from "../ui/screens/squads/SquadEditorView";
import { SquadsView } from "../ui/screens/squads/SquadsView";
import { SquadView } from "../ui/screens/squads/SquadView";
import { SavedViewView } from "../ui/screens/views/SavedViewView";
import { ViewsView } from "../ui/screens/views/ViewsView";
import { useStrings } from "../ui/strings";
import { useTheme } from "../ui/theme";
import * as fx from "./fixtures-workspace";

// Gallery entries for the screens reached from the workspace menu.

const noop = () => {};

function MoreTabs() {
  const strings = useStrings();
  return <TabBar tabs={tabItems(strings, true)} active="more" onSelect={noop} onSearch={noop} searchLabel={strings.search.title} />;
}

/** The app presents editors as page sheets over the screen that opened them. */
function Sheet({ behind, children }: { behind: ReactNode; children: ReactNode }) {
  const palette = useTheme();
  return (
    <>
      {behind}
      <View style={[StyleSheet.absoluteFill, { backgroundColor: "rgba(0,0,0,0.3)" }]} />
      <View style={[styles.sheet, { backgroundColor: palette.background }]}>{children}</View>
    </>
  );
}

function ProjectEditorPreview({ page, mode = "edit" }: { page: ProjectEditorPage; mode?: "create" | "edit" }) {
  const [draft, setDraft] = useState(mode === "create" ? { ...fx.projectDraft, icon: "", title: "", description: "", lead: null, start: null, due: null, status: "planned" as const, priority: "none" as const } : fx.projectDraft);
  return (
    <Sheet behind={<ProjectsView state="ready" sections={fx.projectSections} onRetry={noop} onOpen={noop} onCreate={noop} />}>
      <ProjectEditorView mode={mode} draft={draft} onChange={(patch) => setDraft({ ...draft, ...patch })} page={page} onPage={noop} leads={fx.roster} busy={false} onCancel={noop} onSave={noop} />
    </Sheet>
  );
}

function SquadEditorPreview({ page }: { page: SquadEditorPage }) {
  const [draft, setDraft] = useState(fx.squadDraft);
  const agents = fx.roster.filter((entry) => entry.type === "agent");
  return (
    <Sheet behind={<SquadsView state="ready" squads={fx.squads} onRetry={noop} onOpen={noop} />}>
      <SquadEditorView mode="create" draft={draft} onChange={(patch) => setDraft({ ...draft, ...patch })} page={page} onPage={noop} agents={agents} candidates={fx.roster} busy={false} onCancel={noop} onSave={noop} />
    </Sheet>
  );
}

const WEEKDAYS_AT_NINE: ScheduleConfig = { time: { kind: "at", time: "09:00" }, days: { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] }, timezone: "Asia/Shanghai", raw: null };
const MON_THU_AT_TEN: ScheduleConfig = { ...WEEKDAYS_AT_NINE, time: { kind: "at", time: "10:30" }, days: { kind: "weekly", daysOfWeek: [1, 4] } };

const autopilotsBehind = <AutopilotsView state="ready" sections={fx.autopilotSections} onRetry={noop} onOpen={noop} onCreate={noop} />;

function AutopilotEditorPreview({ page }: { page: AutopilotEditorPage }) {
  const [draft, setDraft] = useState<AutopilotDraft>({
    title: "每日站会摘要",
    description: "汇总昨天合并的 PR、进行中的任务和阻塞项，整理成一条站会摘要。",
    assignee: "agent:lambda",
    mode: "create_issue",
    project: "p1",
    schedule: WEEKDAYS_AT_NINE,
  });
  return (
    <Sheet behind={autopilotsBehind}>
      <AutopilotEditorView
        mode="create"
        draft={draft}
        onChange={(patch) => setDraft({ ...draft, ...patch })}
        page={page}
        onPage={noop}
        assignees={fx.assigneeOptions}
        projects={fx.projectOptions}
        nextRuns={fx.nextRuns}
        onAddSchedule={noop}
        busy={false}
        onCancel={noop}
        onSave={noop}
      />
    </Sheet>
  );
}

function TriggerEditorPreview() {
  const [config, setConfig] = useState(MON_THU_AT_TEN);
  return (
    <Sheet behind={autopilotsBehind}>
      <TriggerEditorView editing config={config} onChange={setConfig} nextRuns={{ state: "ready", items: ["周一 10:30", "周四 10:30", "11月23日 10:30"] }} busy={false} onCancel={noop} onSave={noop} />
    </Sheet>
  );
}

export const WORKSPACE_SCREENS: Record<string, { caption: string; render: () => ReactNode }> = {
  projects: {
    caption: "Projects",
    render: () => (
      <>
        <ProjectsView state="ready" sections={fx.projectSections} onRetry={noop} onOpen={noop} onCreate={noop} />
        <MoreTabs />
      </>
    ),
  },
  "projects-empty": {
    caption: "Projects · empty",
    render: () => (
      <>
        <ProjectsView state="ready" sections={[]} onRetry={noop} onOpen={noop} onCreate={noop} />
        <MoreTabs />
      </>
    ),
  },
  project: {
    caption: "Project",
    render: () => (
      <ProjectView
        state="ready"
        project={fx.project}
        progress={fx.projectProgress}
        resources={fx.projectResources}
        issues={{ state: "ready", groups: fx.projectIssueGroups }}
        onBack={noop}
        onRetry={noop}
        onMore={noop}
        onStatus={noop}
        onPriority={noop}
        onLead={noop}
        onDates={noop}
        onOpenResource={noop}
        onOpenIssue={noop}
      />
    ),
  },
  "project-new": { caption: "New project", render: () => <ProjectEditorPreview page="form" mode="create" /> },
  "project-edit": { caption: "Edit project", render: () => <ProjectEditorPreview page="form" /> },
  "project-lead": { caption: "Edit project · lead", render: () => <ProjectEditorPreview page="lead" /> },
  squads: {
    caption: "Squads",
    render: () => (
      <>
        <SquadsView state="ready" squads={fx.squads} onRetry={noop} onOpen={noop} onCreate={noop} />
        <MoreTabs />
      </>
    ),
  },
  squad: {
    caption: "Squad",
    render: () => (
      <SquadView
        state="ready"
        squad={fx.squad}
        members={fx.squadMembers}
        recent={{ state: "ready", issues: fx.squadRecent }}
        canManage
        onBack={noop}
        onRetry={noop}
        onMore={noop}
        onAddMember={noop}
        onMember={noop}
        onOpenIssue={noop}
      />
    ),
  },
  "squad-new": { caption: "New squad", render: () => <SquadEditorPreview page="form" /> },
  "squad-members": { caption: "New squad · members", render: () => <SquadEditorPreview page="members" /> },
  views: {
    caption: "Views",
    render: () => <ViewsView state="ready" starred={fx.starredViews} others={fx.otherViews} onBack={noop} onRetry={noop} onOpen={noop} onToggleStar={noop} />,
  },
  "saved-view": {
    caption: "Saved view",
    render: () => (
      <SavedViewView
        state="ready"
        title={fx.savedViewName}
        subtitle="我的视图 · 5 个任务"
        filters={fx.savedViewFilters}
        groups={fx.savedViewGroups}
        starred
        onToggleStar={noop}
        onBack={noop}
        onRetry={noop}
        onOpen={noop}
      />
    ),
  },
  autopilots: {
    caption: "Autopilots",
    render: () => (
      <>
        {autopilotsBehind}
        <MoreTabs />
      </>
    ),
  },
  autopilot: {
    caption: "Autopilot",
    render: () => (
      <AutopilotView
        state="ready"
        autopilot={fx.autopilot}
        triggers={fx.autopilotTriggers}
        runs={{ state: "ready", items: fx.autopilotRuns }}
        canWrite
        onBack={noop}
        onRetry={noop}
        onMore={noop}
        onRunNow={noop}
        onTogglePause={noop}
        onAddTrigger={noop}
        onTrigger={noop}
        onToggleTrigger={noop}
        onOpenRun={noop}
      />
    ),
  },
  "autopilot-new": { caption: "New autopilot", render: () => <AutopilotEditorPreview page="form" /> },
  "autopilot-schedule": { caption: "New autopilot · schedule", render: () => <AutopilotEditorPreview page="schedule" /> },
  "autopilot-assignee": { caption: "New autopilot · assignee", render: () => <AutopilotEditorPreview page="assignee" /> },
  "trigger-edit": { caption: "Edit schedule trigger", render: () => <TriggerEditorPreview /> },
};

const styles = StyleSheet.create({
  sheet: { position: "absolute", left: 0, right: 0, bottom: 0, top: 64, borderTopLeftRadius: 12, borderTopRightRadius: 12, overflow: "hidden" },
});

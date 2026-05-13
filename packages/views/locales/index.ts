import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";
import enCommon from "./en/common.json";
import enAuth from "./en/auth.json";
import enSettings from "./en/settings.json";
import enIssues from "./en/issues.json";
import enAgents from "./en/agents.json";
import enEditor from "./en/editor.json";
import enOnboarding from "./en/onboarding.json";
import enInvite from "./en/invite.json";
import enLabels from "./en/labels.json";
import enMembers from "./en/members.json";
import enMyIssues from "./en/my-issues.json";
import enSearch from "./en/search.json";
import enInbox from "./en/inbox.json";
import enWorkspace from "./en/workspace.json";
import enProjects from "./en/projects.json";
import enAutopilots from "./en/autopilots.json";
import enSkills from "./en/skills.json";
import enChat from "./en/chat.json";
import enModals from "./en/modals.json";
import enRuntimes from "./en/runtimes.json";
import enLayout from "./en/layout.json";
import enUsage from "./en/usage.json";
import enUi from "./en/ui.json";
import enSquads from "./en/squads.json";
import enTimeTracking from "./en/time-tracking.json";
import enWorkCalendars from "./en/work-calendars.json";
import enRedmine from "./en/redmine.json";
import enIntegrations from "./en/integrations.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansSettings from "./zh-Hans/settings.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansAgents from "./zh-Hans/agents.json";
import zhHansEditor from "./zh-Hans/editor.json";
import zhHansOnboarding from "./zh-Hans/onboarding.json";
import zhHansInvite from "./zh-Hans/invite.json";
import zhHansLabels from "./zh-Hans/labels.json";
import zhHansMembers from "./zh-Hans/members.json";
import zhHansMyIssues from "./zh-Hans/my-issues.json";
import zhHansSearch from "./zh-Hans/search.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansWorkspace from "./zh-Hans/workspace.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansAutopilots from "./zh-Hans/autopilots.json";
import zhHansSkills from "./zh-Hans/skills.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansModals from "./zh-Hans/modals.json";
import zhHansRuntimes from "./zh-Hans/runtimes.json";
import zhHansLayout from "./zh-Hans/layout.json";
import zhHansUsage from "./zh-Hans/usage.json";
import zhHansUi from "./zh-Hans/ui.json";
import zhHansSquads from "./zh-Hans/squads.json";
import zhHansTimeTracking from "./zh-Hans/time-tracking.json";
import zhHansWorkCalendars from "./zh-Hans/work-calendars.json";
import zhHansRedmine from "./zh-Hans/redmine.json";
import zhHansIntegrations from "./zh-Hans/integrations.json";
import esCommon from "./es/common.json";
import esAuth from "./es/auth.json";
import esSettings from "./es/settings.json";
import esIssues from "./es/issues.json";
import esAgents from "./es/agents.json";
import esEditor from "./es/editor.json";
import esOnboarding from "./es/onboarding.json";
import esInvite from "./es/invite.json";
import esLabels from "./es/labels.json";
import esMembers from "./es/members.json";
import esMyIssues from "./es/my-issues.json";
import esSearch from "./es/search.json";
import esInbox from "./es/inbox.json";
import esWorkspace from "./es/workspace.json";
import esProjects from "./es/projects.json";
import esAutopilots from "./es/autopilots.json";
import esSkills from "./es/skills.json";
import esChat from "./es/chat.json";
import esModals from "./es/modals.json";
import esRuntimes from "./es/runtimes.json";
import esLayout from "./es/layout.json";
import esUsage from "./es/usage.json";
import esUi from "./es/ui.json";
import esSquads from "./es/squads.json";
import esTimeTracking from "./es/time-tracking.json";
import esWorkCalendars from "./es/work-calendars.json";
import esRedmine from "./es/redmine.json";
import esIntegrations from "./es/integrations.json";

// Single source of truth for the resource bundle. Both apps (web layout +
// desktop App.tsx) import from here so adding a locale or namespace happens
// in exactly one place.
export const RESOURCES: Record<SupportedLocale, LocaleResources> = {
  en: {
    common: enCommon,
    auth: enAuth,
    settings: enSettings,
    issues: enIssues,
    agents: enAgents,
    editor: enEditor,
    onboarding: enOnboarding,
    invite: enInvite,
    labels: enLabels,
    members: enMembers,
    "my-issues": enMyIssues,
    search: enSearch,
    inbox: enInbox,
    workspace: enWorkspace,
    projects: enProjects,
    autopilots: enAutopilots,
    skills: enSkills,
    chat: enChat,
    modals: enModals,
    runtimes: enRuntimes,
    layout: enLayout,
    usage: enUsage,
    ui: enUi,
    squads: enSquads,
    "time-tracking": enTimeTracking,
    "work-calendars": enWorkCalendars,
    redmine: enRedmine,
    integrations: enIntegrations,
  },
  "zh-Hans": {
    common: zhHansCommon,
    auth: zhHansAuth,
    settings: zhHansSettings,
    issues: zhHansIssues,
    agents: zhHansAgents,
    editor: zhHansEditor,
    onboarding: zhHansOnboarding,
    invite: zhHansInvite,
    labels: zhHansLabels,
    members: zhHansMembers,
    "my-issues": zhHansMyIssues,
    search: zhHansSearch,
    inbox: zhHansInbox,
    workspace: zhHansWorkspace,
    projects: zhHansProjects,
    autopilots: zhHansAutopilots,
    skills: zhHansSkills,
    chat: zhHansChat,
    modals: zhHansModals,
    runtimes: zhHansRuntimes,
    layout: zhHansLayout,
    usage: zhHansUsage,
    ui: zhHansUi,
    squads: zhHansSquads,
    "time-tracking": zhHansTimeTracking,
    "work-calendars": zhHansWorkCalendars,
    redmine: zhHansRedmine,
    integrations: zhHansIntegrations,
  },
  es: {
    common: esCommon,
    auth: esAuth,
    settings: esSettings,
    issues: esIssues,
    agents: esAgents,
    editor: esEditor,
    onboarding: esOnboarding,
    invite: esInvite,
    labels: esLabels,
    members: esMembers,
    "my-issues": esMyIssues,
    search: esSearch,
    inbox: esInbox,
    workspace: esWorkspace,
    projects: esProjects,
    autopilots: esAutopilots,
    skills: esSkills,
    chat: esChat,
    modals: esModals,
    runtimes: esRuntimes,
    layout: esLayout,
    usage: esUsage,
    ui: esUi,
    squads: esSquads,
    "time-tracking": esTimeTracking,
    "work-calendars": esWorkCalendars,
    redmine: esRedmine,
    integrations: esIntegrations,
  },
};

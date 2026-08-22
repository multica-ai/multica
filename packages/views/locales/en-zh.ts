import type { LocaleResources } from "@multica/core/i18n";
import enAgents from "./en/agents.json";
import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enEditor from "./en/editor.json";
import enInbox from "./en/inbox.json";
import enIssues from "./en/issues.json";
import enLabels from "./en/labels.json";
import enLayout from "./en/layout.json";
import enMembers from "./en/members.json";
import enModals from "./en/modals.json";
import enMyIssues from "./en/my-issues.json";
import enProjects from "./en/projects.json";
import enRuntimes from "./en/runtimes.json";
import enSearch from "./en/search.json";
import enSettings from "./en/settings.json";
import enSquads from "./en/squads.json";
import enWorkspace from "./en/workspace.json";
import zhHansAgents from "./zh-Hans/agents.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansEditor from "./zh-Hans/editor.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansLabels from "./zh-Hans/labels.json";
import zhHansLayout from "./zh-Hans/layout.json";
import zhHansMembers from "./zh-Hans/members.json";
import zhHansModals from "./zh-Hans/modals.json";
import zhHansMyIssues from "./zh-Hans/my-issues.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansRuntimes from "./zh-Hans/runtimes.json";
import zhHansSearch from "./zh-Hans/search.json";
import zhHansSettings from "./zh-Hans/settings.json";
import zhHansSquads from "./zh-Hans/squads.json";
import zhHansWorkspace from "./zh-Hans/workspace.json";

// Data-only subset for Mobile. Keeping this separate avoids pulling the
// Japanese and Korean bundles, or any React view code, into the native bundle.
export const EN_ZH_RESOURCES: Record<"en" | "zh-Hans", LocaleResources> = {
  en: {
    agents: enAgents,
    auth: enAuth,
    chat: enChat,
    common: enCommon,
    editor: enEditor,
    inbox: enInbox,
    issues: enIssues,
    labels: enLabels,
    layout: enLayout,
    members: enMembers,
    modals: enModals,
    "my-issues": enMyIssues,
    projects: enProjects,
    runtimes: enRuntimes,
    search: enSearch,
    settings: enSettings,
    squads: enSquads,
    workspace: enWorkspace,
  },
  "zh-Hans": {
    agents: zhHansAgents,
    auth: zhHansAuth,
    chat: zhHansChat,
    common: zhHansCommon,
    editor: zhHansEditor,
    inbox: zhHansInbox,
    issues: zhHansIssues,
    labels: zhHansLabels,
    layout: zhHansLayout,
    members: zhHansMembers,
    modals: zhHansModals,
    "my-issues": zhHansMyIssues,
    projects: zhHansProjects,
    runtimes: zhHansRuntimes,
    search: zhHansSearch,
    settings: zhHansSettings,
    squads: zhHansSquads,
    workspace: zhHansWorkspace,
  },
};

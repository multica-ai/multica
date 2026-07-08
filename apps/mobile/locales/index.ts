import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enInbox from "./en/inbox.json";
import enIssues from "./en/issues.json";
import enProjects from "./en/projects.json";
import enSettings from "./en/settings.json";
import enSkills from "./en/skills.json";
import enWorkspace from "./en/workspace.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansSettings from "./zh-Hans/settings.json";
import zhHansSkills from "./zh-Hans/skills.json";
import zhHansWorkspace from "./zh-Hans/workspace.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    auth: enAuth,
    chat: enChat,
    common: enCommon,
    inbox: enInbox,
    issues: enIssues,
    projects: enProjects,
    settings: enSettings,
    skills: enSkills,
    workspace: enWorkspace,
  },
  "zh-Hans": {
    auth: zhHansAuth,
    chat: zhHansChat,
    common: zhHansCommon,
    inbox: zhHansInbox,
    issues: zhHansIssues,
    projects: zhHansProjects,
    settings: zhHansSettings,
    skills: zhHansSkills,
    workspace: zhHansWorkspace,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;

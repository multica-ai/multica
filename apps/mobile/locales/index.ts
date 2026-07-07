import enAuth from "./en/auth.json";
import enCommon from "./en/common.json";
import enInbox from "./en/inbox.json";
import enIssues from "./en/issues.json";
import enProjects from "./en/projects.json";
import enSettings from "./en/settings.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansSettings from "./zh-Hans/settings.json";

export type SupportedLocale = "en" | "zh-Hans";

export const RESOURCES = {
  en: {
    auth: enAuth,
    common: enCommon,
    inbox: enInbox,
    issues: enIssues,
    projects: enProjects,
    settings: enSettings,
  },
  "zh-Hans": {
    auth: zhHansAuth,
    common: zhHansCommon,
    inbox: zhHansInbox,
    issues: zhHansIssues,
    projects: zhHansProjects,
    settings: zhHansSettings,
  },
} satisfies Record<SupportedLocale, Record<string, Record<string, unknown>>>;

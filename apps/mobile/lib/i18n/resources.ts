import authEn from "@/locales/en/auth.json";
import chatEn from "@/locales/en/chat.json";
import commonEn from "@/locales/en/common.json";
import editorEn from "@/locales/en/editor.json";
import inboxEn from "@/locales/en/inbox.json";
import issuesEn from "@/locales/en/issues.json";
import navigationEn from "@/locales/en/navigation.json";
import projectsEn from "@/locales/en/projects.json";
import settingsEn from "@/locales/en/settings.json";
import workspaceEn from "@/locales/en/workspace.json";
import authZh from "@/locales/zh-Hans/auth.json";
import chatZh from "@/locales/zh-Hans/chat.json";
import commonZh from "@/locales/zh-Hans/common.json";
import editorZh from "@/locales/zh-Hans/editor.json";
import inboxZh from "@/locales/zh-Hans/inbox.json";
import issuesZh from "@/locales/zh-Hans/issues.json";
import navigationZh from "@/locales/zh-Hans/navigation.json";
import projectsZh from "@/locales/zh-Hans/projects.json";
import settingsZh from "@/locales/zh-Hans/settings.json";
import workspaceZh from "@/locales/zh-Hans/workspace.json";
import authZhHant from "@/locales/zh-Hant/auth.json";
import chatZhHant from "@/locales/zh-Hant/chat.json";
import commonZhHant from "@/locales/zh-Hant/common.json";
import editorZhHant from "@/locales/zh-Hant/editor.json";
import inboxZhHant from "@/locales/zh-Hant/inbox.json";
import issuesZhHant from "@/locales/zh-Hant/issues.json";
import navigationZhHant from "@/locales/zh-Hant/navigation.json";
import projectsZhHant from "@/locales/zh-Hant/projects.json";
import settingsZhHant from "@/locales/zh-Hant/settings.json";
import workspaceZhHant from "@/locales/zh-Hant/workspace.json";

export const resources = {
  en: {
    auth: authEn,
    chat: chatEn,
    common: commonEn,
    editor: editorEn,
    inbox: inboxEn,
    issues: issuesEn,
    navigation: navigationEn,
    projects: projectsEn,
    settings: settingsEn,
    workspace: workspaceEn,
  },
  "zh-Hans": {
    auth: authZh,
    chat: chatZh,
    common: commonZh,
    editor: editorZh,
    inbox: inboxZh,
    issues: issuesZh,
    navigation: navigationZh,
    projects: projectsZh,
    settings: settingsZh,
    workspace: workspaceZh,
  },
  "zh-Hant": {
    auth: authZhHant,
    chat: chatZhHant,
    common: commonZhHant,
    editor: editorZhHant,
    inbox: inboxZhHant,
    issues: issuesZhHant,
    navigation: navigationZhHant,
    projects: projectsZhHant,
    settings: settingsZhHant,
    workspace: workspaceZhHant,
  },
} as const;

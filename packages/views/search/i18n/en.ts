import type { SearchDict } from "./types";

export function createEnDict(): SearchDict {
  return {
    trigger: {
      label: "Search...",
    },
    dialog: {
      title: "Search",
      description: "Search pages, issues, and projects",
      inputPlaceholder: "Type a command or search...",
      noResults: "No results found.",
      emptyHint: "Type to search issues and projects",
    },
    groups: {
      pages: "Pages",
      commands: "Commands",
      switchWorkspace: "Switch Workspace",
      projects: "Projects",
      issues: "Issues",
      recent: "Recent",
    },
    navPages: {
      inbox: "Inbox",
      myIssues: "My Issues",
      issues: "Issues",
      projects: "Projects",
      agents: "Agents",
      runtimes: "Runtimes",
      skills: "Skills",
      settings: "Settings",
    },
    commands: {
      newIssue: "New Issue",
      newProject: "New Project",
      copyIssueLink: "Copy Issue Link",
      copyIdentifier: (identifier) => `Copy Identifier (${identifier})`,
      switchToLight: "Switch to Light Theme",
      switchToDark: "Switch to Dark Theme",
      useSystem: "Use System Theme",
      currentTheme: "Current theme",
      linkCopied: "Link copied",
      copiedIdentifier: (identifier) => `Copied ${identifier}`,
    },
  };
}

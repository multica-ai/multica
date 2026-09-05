import type { useT } from "@multica/views/i18n";

export type DesktopTitleKey =
  | "issues"
  | "issue"
  | "projects"
  | "project"
  | "autopilot"
  | "my_issues"
  | "runtimes"
  | "machine"
  | "runtime"
  | "skills"
  | "skill"
  | "agents"
  | "create_agent"
  | "agent"
  | "member"
  | "squads"
  | "squad"
  | "inbox"
  | "chat"
  | "attachment"
  | "usage"
  | "settings";

export type LayoutTranslator = ReturnType<typeof useT<"layout">>["t"];

export function desktopTitle(
  key: DesktopTitleKey,
  t: LayoutTranslator,
): string {
  switch (key) {
    case "issues":
      return t(($) => $.nav.issues);
    case "issue":
      return t(($) => $.tab.issue);
    case "projects":
      return t(($) => $.nav.projects);
    case "project":
      return t(($) => $.tab.project);
    case "autopilot":
      return t(($) => $.tab.autopilot);
    case "my_issues":
      return t(($) => $.nav.my_issues);
    case "runtimes":
      return t(($) => $.nav.runtimes);
    case "machine":
      return t(($) => $.tab.machine);
    case "runtime":
      return t(($) => $.tab.runtime);
    case "skills":
      return t(($) => $.nav.skills);
    case "skill":
      return t(($) => $.tab.skill);
    case "agents":
      return t(($) => $.nav.agents);
    case "create_agent":
      return t(($) => $.tab.create_agent);
    case "agent":
      return t(($) => $.tab.agent);
    case "member":
      return t(($) => $.tab.member);
    case "squads":
      return t(($) => $.nav.squads);
    case "squad":
      return t(($) => $.tab.squad);
    case "inbox":
      return t(($) => $.nav.inbox);
    case "chat":
      return t(($) => $.nav.chat);
    case "attachment":
      return t(($) => $.tab.attachment);
    case "usage":
      return t(($) => $.nav.usage);
    case "settings":
      return t(($) => $.nav.settings);
  }
}

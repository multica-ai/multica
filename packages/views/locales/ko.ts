import type { LocaleResources } from "@multica/core/i18n";
import koCommon from "./ko/common.json";
import koAuth from "./ko/auth.json";
import koSettings from "./ko/settings.json";
import koIssues from "./ko/issues.json";
import koAgents from "./ko/agents.json";
import koEditor from "./ko/editor.json";
import koOnboarding from "./ko/onboarding.json";
import koInvite from "./ko/invite.json";
import koLabels from "./ko/labels.json";
import koMembers from "./ko/members.json";
import koMyIssues from "./ko/my-issues.json";
import koSearch from "./ko/search.json";
import koInbox from "./ko/inbox.json";
import koWorkspace from "./ko/workspace.json";
import koProjects from "./ko/projects.json";
import koAutopilots from "./ko/autopilots.json";
import koSkills from "./ko/skills.json";
import koChat from "./ko/chat.json";
import koModals from "./ko/modals.json";
import koRuntimes from "./ko/runtimes.json";
import koLayout from "./ko/layout.json";
import koUsage from "./ko/usage.json";
import koUi from "./ko/ui.json";
import koSquads from "./ko/squads.json";
import koBilling from "./ko/billing.json";

const resources: LocaleResources = {
  common: koCommon,
  auth: koAuth,
  settings: koSettings,
  issues: koIssues,
  agents: koAgents,
  editor: koEditor,
  onboarding: koOnboarding,
  invite: koInvite,
  labels: koLabels,
  members: koMembers,
  "my-issues": koMyIssues,
  search: koSearch,
  inbox: koInbox,
  workspace: koWorkspace,
  projects: koProjects,
  autopilots: koAutopilots,
  skills: koSkills,
  chat: koChat,
  modals: koModals,
  runtimes: koRuntimes,
  layout: koLayout,
  usage: koUsage,
  ui: koUi,
  squads: koSquads,
  billing: koBilling,
};

export default resources;

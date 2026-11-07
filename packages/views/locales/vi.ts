import type { LocaleResources } from "@multica/core/i18n";
import viCommon from "./vi/common.json";
import viAuth from "./vi/auth.json";
import viSettings from "./vi/settings.json";
import viIssues from "./vi/issues.json";
import viAgents from "./vi/agents.json";
import viEditor from "./vi/editor.json";
import viOnboarding from "./vi/onboarding.json";
import viInvite from "./vi/invite.json";
import viLabels from "./vi/labels.json";
import viMembers from "./vi/members.json";
import viMyIssues from "./vi/my-issues.json";
import viSearch from "./vi/search.json";
import viInbox from "./vi/inbox.json";
import viWorkspace from "./vi/workspace.json";
import viProjects from "./vi/projects.json";
import viAutopilots from "./vi/autopilots.json";
import viSkills from "./vi/skills.json";
import viChat from "./vi/chat.json";
import viModals from "./vi/modals.json";
import viRuntimes from "./vi/runtimes.json";
import viLayout from "./vi/layout.json";
import viUsage from "./vi/usage.json";
import viUi from "./vi/ui.json";
import viSquads from "./vi/squads.json";
import viBilling from "./vi/billing.json";

const resources: LocaleResources = {
  common: viCommon,
  auth: viAuth,
  settings: viSettings,
  issues: viIssues,
  agents: viAgents,
  editor: viEditor,
  onboarding: viOnboarding,
  invite: viInvite,
  labels: viLabels,
  members: viMembers,
  "my-issues": viMyIssues,
  search: viSearch,
  inbox: viInbox,
  workspace: viWorkspace,
  projects: viProjects,
  autopilots: viAutopilots,
  skills: viSkills,
  chat: viChat,
  modals: viModals,
  runtimes: viRuntimes,
  layout: viLayout,
  usage: viUsage,
  ui: viUi,
  squads: viSquads,
  billing: viBilling,
};

export default resources;

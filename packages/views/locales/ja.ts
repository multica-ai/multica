import type { LocaleResources } from "@multica/core/i18n";
import jaCommon from "./ja/common.json";
import jaAuth from "./ja/auth.json";
import jaSettings from "./ja/settings.json";
import jaIssues from "./ja/issues.json";
import jaAgents from "./ja/agents.json";
import jaEditor from "./ja/editor.json";
import jaOnboarding from "./ja/onboarding.json";
import jaInvite from "./ja/invite.json";
import jaLabels from "./ja/labels.json";
import jaMembers from "./ja/members.json";
import jaMyIssues from "./ja/my-issues.json";
import jaSearch from "./ja/search.json";
import jaInbox from "./ja/inbox.json";
import jaWorkspace from "./ja/workspace.json";
import jaProjects from "./ja/projects.json";
import jaAutopilots from "./ja/autopilots.json";
import jaSkills from "./ja/skills.json";
import jaChat from "./ja/chat.json";
import jaModals from "./ja/modals.json";
import jaRuntimes from "./ja/runtimes.json";
import jaLayout from "./ja/layout.json";
import jaUsage from "./ja/usage.json";
import jaUi from "./ja/ui.json";
import jaSquads from "./ja/squads.json";
import jaBilling from "./ja/billing.json";

const resources: LocaleResources = {
  common: jaCommon,
  auth: jaAuth,
  settings: jaSettings,
  issues: jaIssues,
  agents: jaAgents,
  editor: jaEditor,
  onboarding: jaOnboarding,
  invite: jaInvite,
  labels: jaLabels,
  members: jaMembers,
  "my-issues": jaMyIssues,
  search: jaSearch,
  inbox: jaInbox,
  workspace: jaWorkspace,
  projects: jaProjects,
  autopilots: jaAutopilots,
  skills: jaSkills,
  chat: jaChat,
  modals: jaModals,
  runtimes: jaRuntimes,
  layout: jaLayout,
  usage: jaUsage,
  ui: jaUi,
  squads: jaSquads,
  billing: jaBilling,
};

export default resources;

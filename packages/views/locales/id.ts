import type { LocaleResources } from "@multica/core/i18n";
import idDocs from "./id/docs.json";
import idCommon from "./id/common.json";
import idAuth from "./id/auth.json";
import idSettings from "./id/settings.json";
import idIssues from "./id/issues.json";
import idAgents from "./id/agents.json";
import idEditor from "./id/editor.json";
import idOnboarding from "./id/onboarding.json";
import idInvite from "./id/invite.json";
import idLabels from "./id/labels.json";
import idMembers from "./id/members.json";
import idMyIssues from "./id/my-issues.json";
import idSearch from "./id/search.json";
import idInbox from "./id/inbox.json";
import idWorkspace from "./id/workspace.json";
import idProjects from "./id/projects.json";
import idAutopilots from "./id/autopilots.json";
import idSkills from "./id/skills.json";
import idChat from "./id/chat.json";
import idModals from "./id/modals.json";
import idRuntimes from "./id/runtimes.json";
import idLayout from "./id/layout.json";
import idUsage from "./id/usage.json";
import idUi from "./id/ui.json";
import idSquads from "./id/squads.json";
import idBilling from "./id/billing.json";

const resources: LocaleResources = {
  docs: idDocs,
  common: idCommon,
  auth: idAuth,
  settings: idSettings,
  issues: idIssues,
  agents: idAgents,
  editor: idEditor,
  onboarding: idOnboarding,
  invite: idInvite,
  labels: idLabels,
  members: idMembers,
  "my-issues": idMyIssues,
  search: idSearch,
  inbox: idInbox,
  workspace: idWorkspace,
  projects: idProjects,
  autopilots: idAutopilots,
  skills: idSkills,
  chat: idChat,
  modals: idModals,
  runtimes: idRuntimes,
  layout: idLayout,
  usage: idUsage,
  ui: idUi,
  squads: idSquads,
  billing: idBilling,
};

export default resources;

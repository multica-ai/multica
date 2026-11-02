import frMessageDelivery from "./fr/message-delivery.json";
import frYouShouldKnow from "./fr/you-should-know.json";
import jaMessageDelivery from "./ja/message-delivery.json";
import jaYouShouldKnow from "./ja/you-should-know.json";
import jaEngineering from "./ja/engineering.json";
import frEngineering from "./fr/engineering.json";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";
import { applyBrandOverrides } from "./brand-overrides";
import enCommon from "./en/common.json";
import enQuota from "./en/quota.json";
import enThinking from "./en/thinking.json";
import enLark from "./en/lark.json";
import enForkUi from "./en/fork-ui.json";
import enDependencies from "./en/dependencies.json";
import enDag from "./en/dag.json";
import enAgentConfig from "./en/agent-config.json";
import enAuth from "./en/auth.json";
import enSettings from "./en/settings.json";
import enIssues from "./en/issues.json";
import enAgents from "./en/agents.json";
import enEditor from "./en/editor.json";
import enOnboarding from "./en/onboarding.json";
import enInvite from "./en/invite.json";
import enLabels from "./en/labels.json";
import enMembers from "./en/members.json";
import enMyIssues from "./en/my-issues.json";
import enSearch from "./en/search.json";
import enInbox from "./en/inbox.json";
import enWorkspace from "./en/workspace.json";
import enProjects from "./en/projects.json";
import enAutopilots from "./en/autopilots.json";
import enSkills from "./en/skills.json";
import enSkillPackages from "./en/skill-packages.json";
import enAgentFolders from "./en/agent-folders.json";
import enChat from "./en/chat.json";
import enModals from "./en/modals.json";
import enRuntimes from "./en/runtimes.json";
import enLayout from "./en/layout.json";
import enUsage from "./en/usage.json";
import enUi from "./en/ui.json";
import enSquads from "./en/squads.json";
import enBilling from "./en/billing.json";
import enMessageDelivery from "./en/message-delivery.json";
import enYouShouldKnow from "./en/you-should-know.json";
import enEngineering from "./en/engineering.json";
import zhHansCommon from "./zh-Hans/common.json";
import zhHansQuota from "./zh-Hans/quota.json";
import zhHansThinking from "./zh-Hans/thinking.json";
import zhHansLark from "./zh-Hans/lark.json";
import zhHansForkUi from "./zh-Hans/fork-ui.json";
import zhHansDependencies from "./zh-Hans/dependencies.json";
import zhHansDag from "./zh-Hans/dag.json";
import zhHansAgentConfig from "./zh-Hans/agent-config.json";
import zhHansAuth from "./zh-Hans/auth.json";
import zhHansSettings from "./zh-Hans/settings.json";
import zhHansIssues from "./zh-Hans/issues.json";
import zhHansAgents from "./zh-Hans/agents.json";
import zhHansEditor from "./zh-Hans/editor.json";
import zhHansOnboarding from "./zh-Hans/onboarding.json";
import zhHansInvite from "./zh-Hans/invite.json";
import zhHansLabels from "./zh-Hans/labels.json";
import zhHansMembers from "./zh-Hans/members.json";
import zhHansMyIssues from "./zh-Hans/my-issues.json";
import zhHansSearch from "./zh-Hans/search.json";
import zhHansInbox from "./zh-Hans/inbox.json";
import zhHansWorkspace from "./zh-Hans/workspace.json";
import zhHansProjects from "./zh-Hans/projects.json";
import zhHansAutopilots from "./zh-Hans/autopilots.json";
import zhHansSkills from "./zh-Hans/skills.json";
import zhHansSkillPackages from "./zh-Hans/skill-packages.json";
import zhHansAgentFolders from "./zh-Hans/agent-folders.json";
import zhHansChat from "./zh-Hans/chat.json";
import zhHansModals from "./zh-Hans/modals.json";
import zhHansRuntimes from "./zh-Hans/runtimes.json";
import zhHansLayout from "./zh-Hans/layout.json";
import zhHansUsage from "./zh-Hans/usage.json";
import zhHansUi from "./zh-Hans/ui.json";
import zhHansSquads from "./zh-Hans/squads.json";
import zhHansBilling from "./zh-Hans/billing.json";
import zhHansMessageDelivery from "./zh-Hans/message-delivery.json";
import zhHansYouShouldKnow from "./zh-Hans/you-should-know.json";
import zhHansEngineering from "./zh-Hans/engineering.json";
import zhHantCommon from "./zh-Hant/common.json";
import zhHantAuth from "./zh-Hant/auth.json";
import zhHantSettings from "./zh-Hant/settings.json";
import zhHantIssues from "./zh-Hant/issues.json";
import zhHantAgents from "./zh-Hant/agents.json";
import zhHantEditor from "./zh-Hant/editor.json";
import zhHantOnboarding from "./zh-Hant/onboarding.json";
import zhHantInvite from "./zh-Hant/invite.json";
import zhHantLabels from "./zh-Hant/labels.json";
import zhHantMembers from "./zh-Hant/members.json";
import zhHantMyIssues from "./zh-Hant/my-issues.json";
import zhHantSearch from "./zh-Hant/search.json";
import zhHantInbox from "./zh-Hant/inbox.json";
import zhHantWorkspace from "./zh-Hant/workspace.json";
import zhHantProjects from "./zh-Hant/projects.json";
import zhHantAutopilots from "./zh-Hant/autopilots.json";
import zhHantSkills from "./zh-Hant/skills.json";
import zhHantChat from "./zh-Hant/chat.json";
import zhHantModals from "./zh-Hant/modals.json";
import zhHantRuntimes from "./zh-Hant/runtimes.json";
import zhHantLayout from "./zh-Hant/layout.json";
import zhHantUsage from "./zh-Hant/usage.json";
import zhHantUi from "./zh-Hant/ui.json";
import zhHantSquads from "./zh-Hant/squads.json";
import zhHantBilling from "./zh-Hant/billing.json";
import koCommon from "./ko/common.json";
import koQuota from "./ko/quota.json";
import koThinking from "./ko/thinking.json";
import koLark from "./ko/lark.json";
import koForkUi from "./ko/fork-ui.json";
import koDependencies from "./ko/dependencies.json";
import koDag from "./ko/dag.json";
import koAgentConfig from "./ko/agent-config.json";
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
import koSkillPackages from "./ko/skill-packages.json";
import koAgentFolders from "./ko/agent-folders.json";
import koChat from "./ko/chat.json";
import koModals from "./ko/modals.json";
import koRuntimes from "./ko/runtimes.json";
import koLayout from "./ko/layout.json";
import koUsage from "./ko/usage.json";
import koUi from "./ko/ui.json";
import koSquads from "./ko/squads.json";
import koBilling from "./ko/billing.json";
import koMessageDelivery from "./ko/message-delivery.json";
import koYouShouldKnow from "./ko/you-should-know.json";
import koEngineering from "./ko/engineering.json";
import jaCommon from "./ja/common.json";
import jaQuota from "./ja/quota.json";
import jaThinking from "./ja/thinking.json";
import jaLark from "./ja/lark.json";
import jaForkUi from "./ja/fork-ui.json";
import jaDependencies from "./ja/dependencies.json";
import jaDag from "./ja/dag.json";
import jaAgentConfig from "./ja/agent-config.json";
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
import jaSkillPackages from "./ja/skill-packages.json";
import jaAgentFolders from "./ja/agent-folders.json";
import jaChat from "./ja/chat.json";
import jaModals from "./ja/modals.json";
import jaRuntimes from "./ja/runtimes.json";
import jaLayout from "./ja/layout.json";
import jaUsage from "./ja/usage.json";
import jaUi from "./ja/ui.json";
import jaSquads from "./ja/squads.json";
import jaBilling from "./ja/billing.json";
import frCommon from "./fr/common.json";
import frQuota from "./fr/quota.json";
import frThinking from "./fr/thinking.json";
import frLark from "./fr/lark.json";
import frForkUi from "./fr/fork-ui.json";
import frDependencies from "./fr/dependencies.json";
import frDag from "./fr/dag.json";
import frAgentConfig from "./fr/agent-config.json";
import frAuth from "./fr/auth.json";
import frSettings from "./fr/settings.json";
import frIssues from "./fr/issues.json";
import frAgents from "./fr/agents.json";
import frEditor from "./fr/editor.json";
import frOnboarding from "./fr/onboarding.json";
import frInvite from "./fr/invite.json";
import frLabels from "./fr/labels.json";
import frMembers from "./fr/members.json";
import frMyIssues from "./fr/my-issues.json";
import frSearch from "./fr/search.json";
import frInbox from "./fr/inbox.json";
import frWorkspace from "./fr/workspace.json";
import frProjects from "./fr/projects.json";
import frAutopilots from "./fr/autopilots.json";
import frSkills from "./fr/skills.json";
import frSkillPackages from "./fr/skill-packages.json";
import frAgentFolders from "./fr/agent-folders.json";
import frChat from "./fr/chat.json";
import frModals from "./fr/modals.json";
import frRuntimes from "./fr/runtimes.json";
import frLayout from "./fr/layout.json";
import frUsage from "./fr/usage.json";
import frUi from "./fr/ui.json";
import frSquads from "./fr/squads.json";
import frBilling from "./fr/billing.json";

// Single source of truth for the resource bundle. Both apps (web layout +
// desktop App.tsx) import from here so adding a locale or namespace happens
// in exactly one place.
export const UPSTREAM_RESOURCES: Record<SupportedLocale, LocaleResources> = {
  en: {
    common: enCommon,
    quota: enQuota,
    thinking: enThinking,
    lark: enLark,
    "fork-ui": enForkUi,
    dependencies: enDependencies,
    dag: enDag,
    "agent-config": enAgentConfig,
    auth: enAuth,
    settings: enSettings,
    issues: enIssues,
    agents: enAgents,
    editor: enEditor,
    onboarding: enOnboarding,
    invite: enInvite,
    labels: enLabels,
    members: enMembers,
    "my-issues": enMyIssues,
    search: enSearch,
    inbox: enInbox,
    workspace: enWorkspace,
    projects: enProjects,
    autopilots: enAutopilots,
    skills: enSkills,
    "skill-packages": enSkillPackages,
    "agent-folders": enAgentFolders,
    chat: enChat,
    modals: enModals,
    runtimes: enRuntimes,
    layout: enLayout,
    usage: enUsage,
    ui: enUi,
    squads: enSquads,
    billing: enBilling,
    "message-delivery": enMessageDelivery,
    "you-should-know": enYouShouldKnow,
    engineering: enEngineering,
  },
  "zh-Hans": {
    common: zhHansCommon,
    quota: zhHansQuota,
    thinking: zhHansThinking,
    lark: zhHansLark,
    "fork-ui": zhHansForkUi,
    dependencies: zhHansDependencies,
    dag: zhHansDag,
    "agent-config": zhHansAgentConfig,
    auth: zhHansAuth,
    settings: zhHansSettings,
    issues: zhHansIssues,
    agents: zhHansAgents,
    editor: zhHansEditor,
    onboarding: zhHansOnboarding,
    invite: zhHansInvite,
    labels: zhHansLabels,
    members: zhHansMembers,
    "my-issues": zhHansMyIssues,
    search: zhHansSearch,
    inbox: zhHansInbox,
    workspace: zhHansWorkspace,
    projects: zhHansProjects,
    autopilots: zhHansAutopilots,
    skills: zhHansSkills,
    "skill-packages": zhHansSkillPackages,
    "agent-folders": zhHansAgentFolders,
    chat: zhHansChat,
    modals: zhHansModals,
    runtimes: zhHansRuntimes,
    layout: zhHansLayout,
    usage: zhHansUsage,
    ui: zhHansUi,
    squads: zhHansSquads,
    billing: zhHansBilling,
    "message-delivery": zhHansMessageDelivery,
    "you-should-know": zhHansYouShouldKnow,
    engineering: zhHansEngineering,
  },
  "zh-Hant": {
    common: zhHantCommon,
    auth: zhHantAuth,
    settings: zhHantSettings,
    issues: zhHantIssues,
    agents: zhHantAgents,
    editor: zhHantEditor,
    onboarding: zhHantOnboarding,
    invite: zhHantInvite,
    labels: zhHantLabels,
    members: zhHantMembers,
    "my-issues": zhHantMyIssues,
    search: zhHantSearch,
    inbox: zhHantInbox,
    workspace: zhHantWorkspace,
    projects: zhHantProjects,
    autopilots: zhHantAutopilots,
    skills: zhHantSkills,
    chat: zhHantChat,
    modals: zhHantModals,
    runtimes: zhHantRuntimes,
    layout: zhHantLayout,
    usage: zhHantUsage,
    ui: zhHantUi,
    squads: zhHantSquads,
    billing: zhHantBilling,
  },
  ko: {
    common: koCommon,
    quota: koQuota,
    thinking: koThinking,
    lark: koLark,
    "fork-ui": koForkUi,
    dependencies: koDependencies,
    dag: koDag,
    "agent-config": koAgentConfig,
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
    "skill-packages": koSkillPackages,
    "agent-folders": koAgentFolders,
    chat: koChat,
    modals: koModals,
    runtimes: koRuntimes,
    layout: koLayout,
    usage: koUsage,
    ui: koUi,
    squads: koSquads,
    billing: koBilling,
    "message-delivery": koMessageDelivery,
    "you-should-know": koYouShouldKnow,
    engineering: koEngineering,
  },
  ja: {
    common: jaCommon,
    quota: jaQuota,
    thinking: jaThinking,
    lark: jaLark,
    "fork-ui": jaForkUi,
    dependencies: jaDependencies,
    dag: jaDag,
    "agent-config": jaAgentConfig,
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
    "skill-packages": jaSkillPackages,
    "agent-folders": jaAgentFolders,
    chat: jaChat,
    modals: jaModals,
    runtimes: jaRuntimes,
    layout: jaLayout,
    usage: jaUsage,
    ui: jaUi,
    squads: jaSquads,
    billing: jaBilling,
    "message-delivery": jaMessageDelivery,
    "you-should-know": jaYouShouldKnow,
    engineering: jaEngineering,
  },
  fr: {
    common: frCommon,
    quota: frQuota,
    thinking: frThinking,
    lark: frLark,
    "fork-ui": frForkUi,
    dependencies: frDependencies,
    dag: frDag,
    "agent-config": frAgentConfig,
    auth: frAuth,
    settings: frSettings,
    issues: frIssues,
    agents: frAgents,
    editor: frEditor,
    onboarding: frOnboarding,
    invite: frInvite,
    labels: frLabels,
    members: frMembers,
    "my-issues": frMyIssues,
    search: frSearch,
    inbox: frInbox,
    workspace: frWorkspace,
    projects: frProjects,
    autopilots: frAutopilots,
    skills: frSkills,
    "skill-packages": frSkillPackages,
    "agent-folders": frAgentFolders,
    chat: frChat,
    modals: frModals,
    runtimes: frRuntimes,
    layout: frLayout,
    usage: frUsage,
    ui: frUi,
    squads: frSquads,
    billing: frBilling,
    "message-delivery": frMessageDelivery,
    "you-should-know": frYouShouldKnow,
    engineering: frEngineering,
  },
};

// Web SSR, hydration, desktop and hot resource reloads all consume this same
// effective bundle. Keep upstream keys intact; only the fork owns the overlay.
export const RESOURCES = applyBrandOverrides(UPSTREAM_RESOURCES);

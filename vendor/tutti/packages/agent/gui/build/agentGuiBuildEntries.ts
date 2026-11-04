export const agentGUIBuildEntries = {
  index: "index.ts",
  "agent-gui": "AgentGUI.tsx",
  "startup-shell": "AgentGUIStartupShell.tsx",
  "side-conversation": "side-conversation.ts",
  "side-conversation/controller": "agentSideConversationController.ts",
  "quick-composer": "AgentGUIQuickComposer.tsx",
  "composer-settings-core/index": "composer-settings-core/index.ts",
  agents: "agents.ts",
  "custom-mention": "custom-mention.ts",
  "dock-icons": "dockIcons.ts",
  layout: "layout.ts",
  "mention-search": "agent-gui/agentGuiNode/AgentMentionSearchController.ts",
  "abortable-single-flight": "abortable-single-flight.ts",
  "agent-message-center/index": "agent-message-center/index.ts",
  "agent-conversation/index": "agent-conversation/index.ts",
  "agent-conversation/follow-end":
    "shared/agentConversation/agentConversationFollowEndController.ts",
  "agent-conversation/interactive-answer":
    "shared/agentConversation/interactiveAnswerPayload.ts",
  "agent-env/index": "shared/agentEnv/index.ts",
  "agent-env/ui": "shared/agentEnv/ui.ts",
  "workspace-settings-panel":
    "shared/workspaceSettingsPanel/workspaceSettingsPanelStore.ts",
  "context-mention-palette/index": "context-mention-palette/index.ts",
  "context-mention-provider":
    "agent-gui/agentGuiNode/agentContextMentionProvider.ts",
  "agent-title-text": "shared/utils/agentTitleText.ts",
  "activity-list-projection": "activity-list-projection.ts",
  "conversation-activity-projection": "conversation-activity-projection.ts",
  "provider-identity": "provider-identity.ts",
  "provider-icons": "provider-icons.ts",
  "i18n/index": "i18n/index.ts",
  "mention-file-presentation": "agent-gui/shared/mentionFilePresentation.ts",
  "workbench/index": "workbench/index.ts",
  "workbench/contribution": "workbench/contribution.ts",
  "workbench/launch": "workbench/launch.ts",
  "workbench/providerCatalog": "workbench/providerCatalog.ts",
  "workbench/sessionTitle": "workbench/sessionTitle.ts",
  "workbench/state": "workbench/state.ts",
  "workbench/browser-element-context/index":
    "workbench/browser-element-context/index.ts",
  "workbench/tool-sidebar/index": "workbench/tool-sidebar/index.ts",
  "workbench/types": "workbench/types.ts",
  "workspace-agent-generated-files": "shared/workspaceAgentGeneratedFiles.ts",
  "conversation-message-controller": "agentConversationMessageController.ts",
  "conversation-projection": "conversationProjection.ts",
  "composer-projection": "composerProjection.ts"
} as const;

type AgentGUIBuildEntry = keyof typeof agentGUIBuildEntries;

export const agentGUIDtsBuildEntries = Object.fromEntries(
  Object.entries(agentGUIBuildEntries).map(([name, source]) => [
    name,
    `dist/.dts/${source.replace(/\.tsx?$/, ".d.ts")}`
  ])
) as Readonly<Record<AgentGUIBuildEntry, string>>;

export const agentGUIDtsEntryGroups = [
  ["index"],
  [
    "agent-gui",
    "startup-shell",
    "side-conversation",
    "side-conversation/controller",
    "quick-composer",
    "agents",
    "mention-search",
    "abortable-single-flight",
    "agent-message-center/index",
    "agent-conversation/index",
    "agent-conversation/follow-end",
    "agent-conversation/interactive-answer",
    "context-mention-palette/index",
    "context-mention-provider"
  ],
  [
    "custom-mention",
    "agent-env/index",
    "agent-env/ui",
    "workspace-settings-panel",
    "provider-identity",
    "provider-icons",
    "i18n/index",
    "mention-file-presentation",
    "agent-title-text",
    "activity-list-projection",
    "conversation-activity-projection",
    "workspace-agent-generated-files",
    "conversation-message-controller",
    "conversation-projection",
    "composer-projection",
    "composer-settings-core/index"
  ],
  [
    "dock-icons",
    "layout",
    "workbench/index",
    "workbench/contribution",
    "workbench/launch",
    "workbench/providerCatalog",
    "workbench/sessionTitle",
    "workbench/state",
    "workbench/browser-element-context/index",
    "workbench/tool-sidebar/index",
    "workbench/types"
  ]
] as const satisfies readonly (readonly AgentGUIBuildEntry[])[];

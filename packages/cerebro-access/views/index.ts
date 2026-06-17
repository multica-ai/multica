export { RestrictedLock } from "./components/restricted-lock";
export { RestrictedRef } from "./components/restricted-ref";
export { PrivacyToggle } from "./components/privacy-toggle";
// CEREBRO-PATCH(private-autopilot-exports): owner-only autopilot UI primitives (JEH-1750).
export { PrivateBadge } from "./components/private-badge";
export { AutopilotPrivacySection } from "./components/autopilot-privacy-section";
export { ProjectAccessTab } from "./projects/project-access-tab";
export {
  getMentionAccessContext,
  useEnsureMentionAccessData,
} from "./mention-access";
export { canTriggerPrivateAgentMention } from "./agent-trigger";
// TECH-3670: per-surface agent discovery visibility.
export {
  AGENT_SURFACES,
  SURFACE_LABEL,
  isAgentVisibleOnSurface,
  isAgentHiddenOnSurface,
  isFullySurfaceHidden,
  surfaceVisibilityPreset,
  presetToSurfaceMap,
} from "./surface-visibility";
export type {
  AgentSurface,
  SurfaceVisibilityPreset,
} from "./surface-visibility";
export { AgentSurfaceVisibilityPicker } from "./components/agent-surface-visibility-picker";
// FIR-32: "you're starting an agent" send confirm for the comment composers.
export { usePrivateAgentSendConfirm } from "./use-private-agent-send-confirm";
export type { PrivateAgentSendConfirm } from "./use-private-agent-send-confirm";
export {
  pickAgentNeedingConfirm,
  extractMentionedAgentIds,
  resolveCommentTriggerAgentIds,
} from "./private-agent-confirm";

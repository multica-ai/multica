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

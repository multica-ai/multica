// JEH-1006 — Cerebro workspace groups. Headless surface only: types,
// TanStack query helpers, WS-event registration. The Settings UI lives under
// the `./views` subpath so non-UI consumers (cerebro-realtime, server
// integrations) can import the headless API without dragging in React UI
// components — re-exporting `./views` here would pull `cerebro-feature-flags`
// (and its module-load side effects) into the login-page test's resolution
// graph via cerebro-realtime, breaking unrelated suites.
export type {
  CerebroGroup,
  CerebroGroupMember,
  CreateCerebroGroupRequest,
  UpdateCerebroGroupRequest,
} from "./types";
export {
  groupKeys,
  groupListOptions,
  groupMembersOptions,
} from "./queries";
export { registerCerebroGroupHandlers } from "./realtime";

// FIR-2523 — Cerebro Google Workspace identity-source headless surface.
// UI lives under ./views so non-UI consumers (api client, server hooks)
// can import the headless API without dragging in React components.
export type {
  CerebroAuthSettings,
  CerebroAuthSettingsRole,
} from "./types";
export {
  authSettingsSchema,
  EMPTY_AUTH_SETTINGS,
  SUPPORTED_DEFAULT_ROLES,
} from "./types";
export {
  authSettingsKeys,
  authSettingsOptions,
  useUpdateAuthSettings,
} from "./queries";
export type { UpdateAuthSettingsInput } from "./queries";

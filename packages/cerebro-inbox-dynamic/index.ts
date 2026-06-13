// @multica/cerebro-inbox-dynamic — TECH-3413
//
// The Dynamic inbox: a per-user, block-based inbox built on the same data as the
// classic inbox. Gated by the `cerebro_inbox_dynamic` feature flag and chosen
// per user via the inbox mode preference (see @multica/cerebro-inbox).
export { CerebroInboxSwitcher } from "./components/cerebro-inbox-switcher";
export { DynamicInbox } from "./components/dynamic-inbox";
export {
  useInboxLayout,
  INBOX_LAYOUT_KEY,
  INBOX_LAYOUT_MOBILE_KEY,
  USER_INBOX_PRESETS_KEY,
} from "./use-inbox-layout";
export { useDynamicInboxData } from "./use-dynamic-inbox-data";
export {
  SECTION_CATALOG,
  operatorPreset,
  DEFAULT_INBOX_LAYOUT,
  deleteUserInboxPreset,
  makeUserInboxPresetsBlob,
  readUserInboxPresets,
  upsertUserInboxPreset,
  isValidLayout,
  sectionLabel,
  type InboxLayout,
  type InboxTabConfig,
  type InboxSectionConfig,
  type UserInboxPreset,
  type SectionKind,
  type SectionGroupBy,
  type SectionSort,
} from "./layout";
export {
  selectSectionEntries,
  entryMatchesSection,
  type DynInboxEntry,
  type SectionFilterContext,
} from "./section-filter";

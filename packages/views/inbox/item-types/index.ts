// Importing this module registers the built-in inbox item-type renderers as a
// side effect. Import it once (the inbox page does) before resolving a renderer
// via getInboxItemRenderer.
import "./issue-notification";
import "./conversation";

export {
  conversationEntry,
  getInboxItemRenderer,
  hasInboxItemRenderer,
  notificationEntry,
  registerInboxItemType,
  type InboxFeedEntry,
  type InboxItemDetailProps,
  type InboxItemKind,
  type InboxItemRenderer,
  type InboxItemRowProps,
} from "./contract";

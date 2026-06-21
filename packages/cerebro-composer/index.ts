// @multica/cerebro-composer (FIR-1748)
// Two prompt-input types behind one shared implementation:
//   - MessageComposer → Channels, DMs, Chat, and their threads (push/pull)
//   - CommentComposer → issue comments (new + reply)
// Both build on BaseComposer, which itself builds on the shared ContentEditor.

export {
  BaseComposer,
  type BaseComposerProps,
  type ComposerDraftHandle,
  type ComposerHandle,
  type ComposerMentionItem,
  type ComposerPinMode,
  type ComposerDictation,
} from "./base-composer";
export { MessageComposer, type MessageComposerProps } from "./message-composer";
export { CommentComposer, type CommentComposerProps } from "./comment-composer";
export { ChatComposer, type ChatComposerProps } from "./chat-composer";

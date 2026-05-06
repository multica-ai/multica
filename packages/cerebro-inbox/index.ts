// Cerebro extensions over upstream's inbox. The cerebro-flavored InboxPage
// (Phase 6 chunk 10) was retired in the origin/main merge — upstream's
// channel-first inbox is now the single rendering path. This package keeps
// inbox-folder primitives that aren't part of upstream.
export * from "./core/folders";

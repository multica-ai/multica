// The host registers no message hooks (getGlobalHookRunner returns null), so
// these helpers are only reached defensively.
export function fireAndForgetHook(promise) {
  Promise.resolve(promise).catch(() => {});
}

export function buildCanonicalSentMessageHookContext(context) {
  return context;
}

export function toPluginMessageContext(context) {
  return context;
}

export function toPluginMessageSentEvent(context) {
  return context;
}

// Shared copy for the persisted and admission-time runtime ownership failure.
// Keep this in one mobile-owned module because mobile cannot import the web
// locale bundles.
export const RUNTIME_ACCESS_DENIED_RECOVERY_COPY =
  "This agent can't run on its private runtime. Make the runtime public, or rebind/copy the agent to a runtime its owner can use.";

export const RUNTIME_ACCESS_DENIED_BADGE = "No runtime access";

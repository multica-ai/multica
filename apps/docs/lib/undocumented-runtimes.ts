export type RuntimeExemption = {
  reason: string;
  // A key in server/internal/featureflags/keys.go that defaults to off.
  // AGENTS.md keeps features behind such a flag out of the public docs.
  flag?: string;
};

// Runtimes the daemon registers that the docs deliberately leave out, keyed by
// the runtime ID in server/pkg/agent. They are skipped by the environment
// variable coverage test and left out of the published tool count. Remove an
// entry once the runtime is documented in every locale.
export const undocumentedRuntimes: Record<string, RuntimeExemption> = {
  museapp: {
    reason:
      "Muse app remote receptionist (#9155). Whether it is publicly documented awaits a product decision.",
  },
};
